//go:build unix

package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHelperWatchStart is not a test. TestWatchStartStopsDockerEventsOnSignal
// runs the test binary again with this as the only test, and it becomes
// `homebutler watch start` for as long as that test needs it.
func TestHelperWatchStart(t *testing.T) {
	if os.Getenv("HOMEBUTLER_TEST_WATCH_START") != "1" {
		t.Skip("helper process for TestWatchStartStopsDockerEventsOnSignal")
	}
	rootCmd.SetArgs([]string{"watch", "start", "--interval", "5s"})
	if err := Execute("test", "unknown"); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Stopping watch start used to leave its `docker events` child running (#360).
// The signal handler cancelled the monitors and returned at once, and the
// process could exit before exec.CommandContext's goroutine got round to
// killing the child, which was then reparented to pid 1. Under nohup or tmux
// every restart added one more.
//
// This runs watch start for real, with a `docker` on PATH that records its pid
// and then waits like `docker events` does, sends it SIGTERM, and checks the
// child is gone by the time watch start has exited.
func TestWatchStartStopsDockerEventsOnSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a watch start process")
	}

	home := t.TempDir()
	bin := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "docker-events.pid")

	// exec keeps the pid, so the pid written is the one watch start started.
	fakeDocker := "#!/bin/sh\n" +
		"if [ \"$1\" = events ]; then\n" +
		"  echo $$ > \"$FAKE_DOCKER_PIDFILE.tmp\"\n" +
		"  mv \"$FAKE_DOCKER_PIDFILE.tmp\" \"$FAKE_DOCKER_PIDFILE\"\n" +
		"  exec sleep 300\n" +
		"fi\n" +
		"exit 1\n"
	writeFile(t, filepath.Join(bin, "docker"), fakeDocker)
	if err := os.Chmod(filepath.Join(bin, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(home, ".homebutler", "watch", "targets.json"),
		`[{"container":"web","kind":"docker","added_at":"2026-01-01T00:00:00Z"}]`)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, configPath, "")

	helper := exec.Command(os.Args[0], "-test.run=^TestHelperWatchStart$")
	helper.Env = append(os.Environ(),
		"HOMEBUTLER_TEST_WATCH_START=1",
		// One P means nothing else runs between the signal handler returning
		// and the process exiting unless watch start waits for it, so a
		// missing wait fails every time rather than when the race is lost.
		"GOMAXPROCS=1",
		// A race-enabled binary sleeps a second on exit by default, which is
		// long enough to hide the bug under go test -race. Options already
		// set, such as halt_on_error, are kept.
		"GORACE="+strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"),
		"HOME="+home,
		"HOMEBUTLER_CONFIG="+configPath,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DOCKER_HOST=unix:///nonexistent/docker.sock",
		"FAKE_DOCKER_PIDFILE="+pidFile,
	)
	var out strings.Builder
	helper.Stdout = &out
	helper.Stderr = &out
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- helper.Wait() }()
	t.Cleanup(func() {
		_ = helper.Process.Kill()
		<-exited
	})

	var pid int
	deadline := time.Now().Add(30 * time.Second)
	for pid == 0 {
		select {
		case err := <-exited:
			exited <- err
			t.Fatalf("watch start exited before starting docker events: %v\n%s", err, out.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("watch start did not start docker events within 30s\n%s", out.String())
		}
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Whatever happens below, the fake docker events must not outlive the test.
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	if err := helper.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		exited <- err
		if err != nil {
			t.Fatalf("watch start exited with %v after SIGTERM\n%s", err, out.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("watch start did not exit within 30s of SIGTERM\n%s", out.String())
	}

	// Checked once, the moment watch start has exited: being gone then is the
	// whole point. A child that dies later only because something else reaps
	// it is the bug.
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("docker events (pid %d) is still running after watch start exited (kill 0: %v)\n%s", pid, err, out.String())
	}
}
