package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// bubbletea's init asks the terminal for its background colour, and
// internal/termbg's init exists to stop it. This re-runs the test binary,
// which goes through the same package inits as homebutler, with stdout on a
// pty that nobody answers, and checks that no OSC 11 query reaches it.
func TestStartupDoesNotQueryTheTerminal(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) is not available to provide a pty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "script", "-q", "/dev/null", os.Args[0], "-test.run=^$")
	case "linux":
		cmd = exec.CommandContext(ctx, "script", "-qec", "'"+os.Args[0]+"' '-test.run=^$'", "/dev/null")
	default:
		t.Skipf("no script(1) invocation known for %s", runtime.GOOS)
	}
	// termenv never queries when CI is set, which would let this pass on
	// GitHub Actions without the fix.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CI=") && !strings.HasPrefix(kv, "TERM=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the test binary under script: %v\n%q", err, out)
	}
	if bytes.Contains(out, []byte("\x1b]11;")) {
		t.Errorf("startup sent an OSC 11 background query to the terminal: %q", out)
	}
}
