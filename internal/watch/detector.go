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

// CheckTargets runs a one-shot restart check over the watch list.
//
// Only docker targets expose their restart state without a running monitor, so
// systemd and pm2 targets cannot be inspected here. They are reported in
// RunResult.Skipped rather than dropped silently — a caller that printed only
// "no restarts detected" would be claiming those targets are healthy when they
// were never looked at.
// keep caps how many incidents are retained on disk; see SaveIncident.
func CheckTargets(dir string, keep int) (RunResult, error) {
	var result RunResult

	targets, err := LoadTargets(dir)
	if err != nil {
		return result, err
	}
	if len(targets) == 0 {
		return result, nil
	}

	states, err := LoadState(dir)
	if err != nil {
		return result, err
	}

	var incidents []Incident
	now := time.Now()
	type inspection struct {
		result *InspectResult
		err    error
	}
	inspected := map[string]inspection{}
	recorded := map[string]bool{}

	for _, t := range targets {
		if !t.CheckSupported() {
			result.Skipped = append(result.Skipped, SkippedTarget{
				Name: t.Container,
				Kind: t.EffectiveKind(),
			})
			continue
		}
		result.Checked++

		// A docker target's unit is the container's real name. It is what
		// is inspected, and what the incident is recorded under, as the
		// daemon records it and as report snapshots name the container.
		// State stays keyed by the target, which is how watch list joins it.
		name := t.EffectiveUnit()
		// Targets that share a unit are one container. It is inspected
		// once, and a restart of it is one incident, as the daemon, which
		// watches a set of units, records it; one per target would count a
		// single restart towards flapping once for each.
		ins, seen := inspected[name]
		if !seen {
			ins.result, ins.err = inspectContainerFunc(name)
			inspected[name] = ins
			if ins.err != nil {
				fmt.Fprintf(defaultStderr, "warning: %v\n", ins.err)
			}
		}
		if ins.err != nil {
			continue
		}
		curr := ins.result

		prev := states[t.Container]
		if prev != nil && prev.Container != "" && prev.Container != name {
			// Recorded from another container: a check before this one
			// inspected the target's name rather than its unit. That is
			// no baseline for this container, so this check sets one.
			prev = nil
		}
		if ev := DetectRestart(prev, curr); ev != nil && !recorded[name] {
			recorded[name] = true
			postLogs := captureLogsFunc(name, "100")
			inc := Incident{
				ID:           GenerateIncidentID(name, now),
				Kind:         KindDocker,
				Container:    name,
				DetectedAt:   now,
				RestartCount: ev.RestartCount,
				PrevStarted:  ev.PrevStarted,
				CurrStarted:  ev.CurrStarted,
				PreLogs:      "(captured at detection — see post_logs for current state)",
				PostLogs:     postLogs,
			}
			if err := SaveIncident(dir, &inc, keep); err != nil {
				fmt.Fprintf(defaultStderr, "warning: save incident: %v\n", err)
			}
			incidents = append(incidents, inc)
		}

		states[t.Container] = &ContainerState{
			Container:    name,
			RestartCount: curr.RestartCount,
			StartedAt:    curr.StartedAt,
			LastChecked:  now,
		}
	}

	result.Incidents = incidents

	if err := SaveState(dir, states); err != nil {
		return result, fmt.Errorf("save state: %w", err)
	}
	return result, nil
}

// SkippedTarget is a watch target that a one-shot check could not inspect,
// because its kind needs a running monitor rather than a point-in-time lookup.
type SkippedTarget struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type RunResult struct {
	// Checked counts the targets that were actually inspected, not every target
	// on the watch list.
	Checked   int             `json:"checked"`
	Skipped   []SkippedTarget `json:"skipped,omitempty"`
	Incidents []Incident      `json:"incidents,omitempty"`
}

// WatchAddResult is what `watch_add` and `watch add --json` answer with.
// Added is false when the target was already watched, which is not a failure.
type WatchAddResult struct {
	Container string `json:"container"`
	Kind      string `json:"kind"`
	Added     bool   `json:"added"`
}

// WatchRemoveResult is what `watch_remove` and `watch remove --json` answer
// with.
type WatchRemoveResult struct {
	Container string `json:"container"`
	Removed   bool   `json:"removed"`
}
