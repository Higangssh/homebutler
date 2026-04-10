package watch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// SystemdMonitor watches systemd units by polling their state.
type SystemdMonitor struct {
	// Run executes an external command. Required.
	Run CommandRunner

	// Dir is the storage directory for incidents.
	Dir string

	// Interval is the polling interval.
	Interval time.Duration
}

type systemdState struct {
	ActiveState string
	SubState    string
	StartTS     string
}

func (sm *SystemdMonitor) parseState(output string) systemdState {
	s := systemdState{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ActiveState=") {
			s.ActiveState = strings.TrimPrefix(line, "ActiveState=")
		} else if strings.HasPrefix(line, "SubState=") {
			s.SubState = strings.TrimPrefix(line, "SubState=")
		} else if strings.HasPrefix(line, "ExecMainStartTimestamp=") {
			s.StartTS = strings.TrimPrefix(line, "ExecMainStartTimestamp=")
		}
	}
	return s
}

// Watch polls systemd unit states and sends incidents when units fail or stop.
func (sm *SystemdMonitor) Watch(ctx context.Context, targets []Target, incidents chan<- Incident) error {
	if len(targets) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	run := sm.Run
	if run == nil {
		return fmt.Errorf("SystemdMonitor requires a CommandRunner")
	}

	interval := sm.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	// Track previous states
	prev := make(map[string]systemdState, len(targets))

	// Seed initial states
	for _, t := range targets {
		unit := t.EffectiveUnit()
		out, err := run("systemctl", "show", unit,
			"--property=ActiveState,SubState,ExecMainStartTimestamp")
		if err != nil {
			continue
		}
		prev[unit] = sm.parseState(out)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for _, t := range targets {
				unit := t.EffectiveUnit()
				out, err := run("systemctl", "show", unit,
					"--property=ActiveState,SubState,ExecMainStartTimestamp")
				if err != nil {
					continue
				}
				curr := sm.parseState(out)
				old, hasPrev := prev[unit]

				// Only treat "failed" as a failure state.
				// "inactive" is only significant if the unit was previously "active"
				// and the start timestamp changed (indicating a restart/stop).
				isFailed := curr.ActiveState == "failed"
				wasFailed := old.ActiveState == "failed"
				inactiveStopped := curr.ActiveState == "inactive" && old.ActiveState == "active"
				startChanged := hasPrev && curr.StartTS != old.StartTS && old.StartTS != ""

				if (isFailed && !wasFailed) || (inactiveStopped && startChanged) || startChanged {
					// Capture journal logs
					preLogs, _ := run("journalctl", "-u", unit, "-n", "100", "--no-pager")

					now := time.Now()
					inc := Incident{
						ID:          GenerateIncidentID(t.Container, now),
						Container:   t.Container,
						DetectedAt:  now,
						PrevStarted: old.StartTS,
						CurrStarted: curr.StartTS,
						PreLogs:     preLogs,
						PostLogs:    fmt.Sprintf("ActiveState=%s SubState=%s", curr.ActiveState, curr.SubState),
					}
					if sm.Dir != "" {
						if err := SaveIncident(sm.Dir, &inc); err != nil {
							fmt.Fprintf(os.Stderr, "[systemd-monitor] warning: save incident: %v\n", err)
						}
					}
					select {
					case incidents <- inc:
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				prev[unit] = curr
			}
		}
	}
}
