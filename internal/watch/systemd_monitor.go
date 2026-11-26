package watch

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/service"
)

// SystemdMonitor watches systemd units by polling their state.
type SystemdMonitor struct {
	// Run executes an external command. Required.
	Run CommandRunner

	// Dir is the storage directory for incidents.
	Dir string

	// Interval is the polling interval.
	Interval time.Duration

	// Keep caps how many incidents are retained; zero or less keeps everything.
	Keep int
}

type systemdState struct {
	// ExitStatus is the unit's last exit status, nil when systemd did not
	// report one. Zero is a real status, so it cannot be the missing value.
	ExitStatus  *int
	ActiveState string
	SubState    string
	// StartTS is systemd's text, compared as-is to notice a restart; Started
	// is the same moment as a time, or zero when it could not be read.
	StartTS string
	Started time.Time
	// Exited is when the main process last exited, or zero when systemd did
	// not say.
	Exited time.Time
}

// startedRunDied is whether the start systemd reports is that of a run that
// has already failed. A unit that restarts and fails again within one poll
// reports a new start, but it is the start of the run that died, not of a run
// after the crash. The state is asked as well as the exit because systemd's
// timestamps are to the second: a run that started in the second the last one
// exited would otherwise read as over while it is running.
func (s systemdState) startedRunDied() bool {
	return s.ActiveState == "failed" && !s.Exited.IsZero() && !s.Exited.Before(s.Started)
}

// systemdShow reads a unit's state. It asks for unix timestamps, which
// systemd prints as @1790772404, and falls back to the default format on a
// systemd older than 248 that does not know --timestamp.
func systemdShow(run CommandRunner, unit string) (string, error) {
	const props = "--property=ActiveState,SubState,ExecMainStartTimestamp,ExecMainExitTimestamp,ExecMainStatus"
	out, err := run("systemctl", "show", unit, "--timestamp=unix", props)
	if err != nil {
		out, err = run("systemctl", "show", unit, props)
	}
	return out, err
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
			s.Started = service.ParseSystemdTime(s.StartTS)
		} else if strings.HasPrefix(line, "ExecMainExitTimestamp=") {
			s.Exited = service.ParseSystemdTime(strings.TrimPrefix(line, "ExecMainExitTimestamp="))
		} else if strings.HasPrefix(line, "ExecMainStatus=") {
			// systemd reports the unit's own exit status here. It is absent
			// for a unit that has never run, so a parse failure means "not
			// reported" rather than zero (#108).
			if code, err := strconv.Atoi(strings.TrimPrefix(line, "ExecMainStatus=")); err == nil {
				s.ExitStatus = &code
			}
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
		out, err := systemdShow(run, unit)
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
				out, err := systemdShow(run, unit)
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

					// A unit that failed without starting again still reports
					// the start of the run that died. That is not a new run,
					// and written as curr_started_at it reads as one. Nor is
					// a start the unit failed after within the same poll.
					currStarted := ""
					if !curr.Started.Equal(old.Started) && !curr.startedRunDied() {
						currStarted = StartTime(curr.Started)
					}

					now := time.Now()
					inc := Incident{
						ID:          GenerateIncidentID(t.Container, now),
						Kind:        KindSystemd,
						Container:   t.Container,
						DetectedAt:  now,
						PrevStarted: StartTime(old.Started),
						CurrStarted: currStarted,
						PreLogs:     preLogs,
						PostLogs:    fmt.Sprintf("ActiveState=%s SubState=%s", curr.ActiveState, curr.SubState),
						ExitCode:    curr.ExitStatus,
					}
					if sm.Dir != "" {
						if err := SaveIncident(sm.Dir, &inc, sm.Keep); err != nil {
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
