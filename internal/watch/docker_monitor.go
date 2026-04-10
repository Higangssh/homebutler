package watch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

// EventStreamer abstracts the creation of a docker events process for testability.
// It returns an io.ReadCloser for the events stream and a cleanup function.
type EventStreamer func(ctx context.Context) (io.ReadCloser, func(), error)

// DockerMonitor watches Docker containers via `docker events` stream.
type DockerMonitor struct {
	// Run executes an external command. Nil defaults to util.DockerCmd-style execution.
	Run CommandRunner

	// WatchDir is the storage directory for incidents.
	Dir string

	// PostLogDelay is how long to wait after a die event before capturing post-restart logs.
	PostLogDelay time.Duration

	// Events creates the docker events stream. Nil defaults to exec.CommandContext("docker", "events", ...).
	Events EventStreamer
}

type dockerEvent struct {
	Status string           `json:"status"`
	ID     string           `json:"id"`
	Actor  dockerEventActor `json:"Actor"`
	Time   int64            `json:"time"`
}

type dockerEventActor struct {
	Attributes map[string]string `json:"Attributes"`
}

// containerName extracts the container name from a docker event.
func (e *dockerEvent) containerName() string {
	if e.Actor.Attributes != nil {
		if name, ok := e.Actor.Attributes["name"]; ok {
			return name
		}
	}
	return e.ID
}

// Watch starts listening to docker die events and sends Incidents for watched containers.
func (dm *DockerMonitor) Watch(ctx context.Context, targets []Target, incidents chan<- Incident) error {
	if len(targets) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	run := dm.Run
	if run == nil {
		// Default runner: first arg is the binary name (e.g. "docker"),
		// consistent with all other monitors.
		run = func(name string, args ...string) (string, error) {
			return util.RunCmd(name, args...)
		}
	}

	delay := dm.PostLogDelay
	if delay == 0 {
		delay = 5 * time.Second
	}

	// Build a set of watched container names
	watched := make(map[string]bool, len(targets))
	for _, t := range targets {
		watched[t.EffectiveUnit()] = true
	}

	// Start docker events stream
	evStream := dm.Events
	if evStream == nil {
		evStream = func(ctx context.Context) (io.ReadCloser, func(), error) {
			util.EnsureDockerHost()
			cmd := exec.CommandContext(ctx, "docker", "events",
				"--filter", "event=die",
				"--format", "{{json .}}")
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return nil, nil, fmt.Errorf("docker events pipe: %w", err)
			}
			if err := cmd.Start(); err != nil {
				return nil, nil, fmt.Errorf("docker events start: %w", err)
			}
			cleanup := func() { _ = cmd.Wait() }
			return stdout, cleanup, nil
		}
	}

	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, cleanup, err := evStream(cmdCtx)
	if err != nil {
		return err
	}

	// Read events in a goroutine
	eventCh := make(chan dockerEvent, 16)
	errCh := make(chan error, 1)
	scanDone := make(chan struct{})
	go func() {
		defer close(eventCh)
		defer close(scanDone)
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var ev dockerEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			select {
			case eventCh <- ev:
			case <-cmdCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			errCh <- err
		}
	}()

	for {
		select {
		case <-ctx.Done():
			cancel()
			// Wait for the scanner goroutine to finish before calling cleanup
			<-scanDone
			if cleanup != nil {
				cleanup()
			}
			return ctx.Err()
		case ev, ok := <-eventCh:
			if !ok {
				if cleanup != nil {
					cleanup()
				}
				return fmt.Errorf("docker events stream ended")
			}
			name := ev.containerName()
			if !watched[name] {
				continue
			}

			// Capture pre-death logs immediately (the container just died)
			preLogs := captureLogsWithRunner(run, "docker", name, "100")

			now := time.Now()

			// Wait for possible restart, then capture post-restart logs
			postLogs := ""
			select {
			case <-time.After(delay):
				postLogs = captureLogsWithRunner(run, "docker", name, "50")
			case <-ctx.Done():
			}

			inc := Incident{
				ID:          GenerateIncidentID(name, now),
				Container:   name,
				DetectedAt:  now,
				PrevStarted: fmt.Sprintf("died at event time %d", ev.Time),
				CurrStarted: "(post-restart)",
				PreLogs:     preLogs,
				PostLogs:    postLogs,
			}
			if dm.Dir != "" {
				if err := SaveIncident(dm.Dir, &inc); err != nil {
					fmt.Fprintf(os.Stderr, "[docker-monitor] warning: save incident: %v\n", err)
				}
			}
			select {
			case incidents <- inc:
			case <-ctx.Done():
				return ctx.Err()
			}
		case err := <-errCh:
			if cleanup != nil {
				cleanup()
			}
			return fmt.Errorf("docker events read: %w", err)
		}
	}
}

func captureLogsWithRunner(run CommandRunner, binary string, container string, lines string) string {
	out, err := run(binary, "logs", "--tail", lines, container)
	if err != nil {
		return fmt.Sprintf("(failed to capture logs: %v)", err)
	}
	return out
}
