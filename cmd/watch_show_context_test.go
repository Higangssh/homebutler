package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/watch"
)

// runWatchText runs one watch subcommand for a person and returns what it
// printed and the error it returned.
func runWatchText(t *testing.T, args ...string) (string, error) {
	t.Helper()
	savedJSON, savedCfg := jsonOutput, cfg
	t.Cleanup(func() { jsonOutput, cfg = savedJSON, savedCfg })
	jsonOutput = false

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	cmd := newWatchShowCmd()
	cmd.SetArgs(args)
	cmd.SetErr(io.Discard)
	runErr := cmd.Execute()
	os.Stdout = saved
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

// The incident's time and the snapshots' are printed in one convention, UTC
// with its zone, so a reader can put them side by side. --logs prints the
// logs here too, and means nothing without --context, where they are always
// printed, so it is refused there.
func TestWatchShowContextForAPerson(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOMEBUTLER_CONFIG", "")
	dir, _ := watch.WatchDir()
	detected := time.Date(2026, 11, 24, 23, 3, 4, 0, time.FixedZone("KST", 9*3600))
	inc := watch.Incident{ID: "web-1", Container: "web", Kind: watch.KindDocker, DetectedAt: detected, PreLogs: "panic: boom", PostLogs: "starting"}
	if err := watch.SaveIncident(dir, &inc, 0); err != nil {
		t.Fatal(err)
	}

	out, err := runWatchText(t, "--context", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Detected:  2026-11-24T14:03:04Z") {
		t.Errorf("detected time not in UTC with its zone:\n%s", out)
	}
	if strings.Contains(out, "panic: boom") {
		t.Errorf("logs printed without --logs:\n%s", out)
	}

	out, err = runWatchText(t, "--context", "--logs", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "=== Pre-Death Logs ===\npanic: boom") || !strings.Contains(out, "=== Post-Restart Logs ===\nstarting") {
		t.Errorf("--context --logs did not print the logs:\n%s", out)
	}

	if _, err := runWatchText(t, "--logs", "web-1"); err == nil || !strings.Contains(err.Error(), "only applies with --context") {
		t.Errorf("--logs without --context: err = %v, want it refused", err)
	}
}
