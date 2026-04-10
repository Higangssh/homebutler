package watch

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

// inspectContainerFunc and captureLogsFunc are package-level function variables
// used by CheckTargets. They default to InspectContainer and CaptureLogs respectively,
// and can be overridden in tests for isolation.
var inspectContainerFunc = InspectContainer
var captureLogsFunc = CaptureLogs

type InspectResult struct {
	RestartCount int    `json:"restart_count"`
	StartedAt    string `json:"started_at"`
	Running      bool   `json:"running"`
}

type RestartEvent struct {
	Container    string
	RestartCount int
	PrevStarted  string
	CurrStarted  string
}

func InspectContainer(name string) (*InspectResult, error) {
	out, err := util.DockerCmd("inspect",
		"--format", `{"restart_count":{{.RestartCount}},"started_at":"{{.State.StartedAt}}","running":{{.State.Running}}}`,
		name)
	if err != nil {
		return nil, fmt.Errorf("docker inspect %s: %w", name, err)
	}
	var result InspectResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, fmt.Errorf("parse inspect output for %s: %w", name, err)
	}
	return &result, nil
}

func DetectRestart(prev *ContainerState, curr *InspectResult) *RestartEvent {
	if prev == nil {
		return nil
	}
	if curr.RestartCount > prev.RestartCount {
		return &RestartEvent{
			Container:    prev.Container,
			RestartCount: curr.RestartCount,
			PrevStarted:  prev.StartedAt,
			CurrStarted:  curr.StartedAt,
		}
	}
	if curr.StartedAt != prev.StartedAt && prev.StartedAt != "" {
		return &RestartEvent{
			Container:    prev.Container,
			RestartCount: curr.RestartCount,
			PrevStarted:  prev.StartedAt,
			CurrStarted:  curr.StartedAt,
		}
	}
	return nil
}

func CaptureLogs(container string, lines string) string {
	out, err := util.DockerCmd("logs", "--tail", lines, container)
	if err != nil {
		return fmt.Sprintf("(failed to capture logs: %v)", err)
	}
	return out
}

func CheckTargets(dir string) ([]Incident, error) {
	targets, err := LoadTargets(dir)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}

	states, err := LoadState(dir)
	if err != nil {
		return nil, err
	}

	var incidents []Incident
	now := time.Now()

	for _, t := range targets {
		// CheckTargets only handles docker targets; skip others
		if t.EffectiveKind() != "docker" {
			continue
		}
		curr, err := inspectContainerFunc(t.Container)
		if err != nil {
			fmt.Fprintf(defaultStderr, "warning: %v\n", err)
			continue
		}

		prev := states[t.Container]
		if ev := DetectRestart(prev, curr); ev != nil {
			postLogs := captureLogsFunc(t.Container, "100")
			inc := Incident{
				ID:           GenerateIncidentID(t.Container, now),
				Container:    t.Container,
				DetectedAt:   now,
				RestartCount: ev.RestartCount,
				PrevStarted:  ev.PrevStarted,
				CurrStarted:  ev.CurrStarted,
				PreLogs:      "(captured at detection — see post_logs for current state)",
				PostLogs:     postLogs,
			}
			if err := SaveIncident(dir, &inc); err != nil {
				fmt.Fprintf(defaultStderr, "warning: save incident: %v\n", err)
			}
			incidents = append(incidents, inc)
		}

		states[t.Container] = &ContainerState{
			Container:    t.Container,
			RestartCount: curr.RestartCount,
			StartedAt:    curr.StartedAt,
			LastChecked:  now,
		}
	}

	if err := SaveState(dir, states); err != nil {
		return incidents, fmt.Errorf("save state: %w", err)
	}
	return incidents, nil
}

type RunResult struct {
	Checked   int        `json:"checked"`
	Incidents []Incident `json:"incidents,omitempty"`
}
