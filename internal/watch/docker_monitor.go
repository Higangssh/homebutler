package watch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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

	// Keep caps how many incidents are retained; zero or less keeps everything.
	Keep int
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

// exitCode reports the status the container exited with, and whether the event
// carried one. A die event's attributes include it as a decimal string:
//
//	{"execDuration":"2","exitCode":"42","image":"alpine","name":"hb-evt"}
//
// Its absence is reported rather than defaulted, because zero is a meaningful
// exit code — treating a missing value as zero is what made every incident read
// as a clean exit (#108).
func (e *dockerEvent) exitCode() (int, bool) {
	if e.Actor.Attributes == nil {
		return 0, false
	}
	raw, ok := e.Actor.Attributes["exitCode"]
	if !ok {
		return 0, false
	}
	code, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return code, true
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
// reconnectMin and reconnectMax bound how quickly a dropped event stream is
// retried. The supervisor that will run this process (#80) restarts it on exit,
// so a monitor that returned on a dropped stream would turn a docker restart
// into a tight loop of process starts, each writing a line to the log. Retrying
// here keeps the supervisor for the case it is for: the process actually died.
const (
	reconnectMin = time.Second
	reconnectMax = 30 * time.Second
)

// Watch follows docker events for the given targets until ctx is cancelled,
// reconnecting when the stream drops.
//
// `docker events` is a long-lived stream, and it ends for ordinary reasons: the
// daemon restarting, a Docker Desktop update, the socket being recreated. None
// of those mean monitoring should stop, so the stream ending is a reason to
// wait and reconnect rather than a reason to return.
func (dm *DockerMonitor) Watch(ctx context.Context, targets []Target, incidents chan<- Incident) error {
	if len(targets) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	delay := reconnectMin
	for {
		err := dm.watchOnce(ctx, targets, incidents)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "[docker-monitor] %v; reconnecting in %s\n", err, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		// Back off while it keeps failing; a stream that lasted is not evidence
		// of a problem, but this cannot tell the two apart without timing the
		// connection, and erring towards the slower retry costs only latency.
		if delay < reconnectMax {
			delay *= 2
			if delay > reconnectMax {
				delay = reconnectMax
			}
		}
	}
}

// watchOnce follows one event stream until it ends or ctx is cancelled.
func (dm *DockerMonitor) watchOnce(ctx context.Context, targets []Target, incidents chan<- Incident) error {

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
			if code, ok := ev.exitCode(); ok {
				inc.ExitCode = &code
				// 137 is SIGKILL, which the kernel's OOM killer uses. The
				// analyser distinguishes the two, so this only reports what
				// the event actually said.
			}
			if dm.Dir != "" {
				if err := SaveIncident(dm.Dir, &inc, dm.Keep); err != nil {
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
