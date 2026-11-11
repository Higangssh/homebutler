package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/Higangssh/homebutler/internal/watch"
	"github.com/spf13/cobra"
)

// runWatchJSON runs one watch subcommand with --json in a home of its own and
// returns what it printed.
func runWatchJSON(t *testing.T, cmd *cobra.Command, args ...string) []byte {
	t.Helper()
	savedJSON, savedCfg := jsonOutput, cfg
	t.Cleanup(func() { jsonOutput, cfg = savedJSON, savedCfg })
	jsonOutput = true

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	cmd.SetArgs(args)
	runErr := cmd.Execute()
	os.Stdout = saved
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("%s %v: %v\n%s", cmd.Name(), args, runErr, out)
	}
	return out
}

// strictInto fails on a field v does not have, and on anything that is not
// JSON, which is what these printed before.
func strictInto(t *testing.T, out []byte, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("not the tool's answer: %v\n%s", err, out)
	}
}

// watch_add, watch_remove and watch_check pointed at a server hand back what
// these print, so under --json they print the tool's answer. They printed a
// sentence, which the MCP server could not decode.
func TestWatchCommandsPrintTheToolAnswerUnderJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HOMEBUTLER_CONFIG", "")

	var check watch.RunResult
	strictInto(t, runWatchJSON(t, newWatchCheckCmd()), &check)
	if check.Checked != 0 {
		t.Errorf("check with nothing watched = %+v", check)
	}

	var added watch.WatchAddResult
	strictInto(t, runWatchJSON(t, newWatchAddCmd(), "--kind", "pm2", "api"), &added)
	if added != (watch.WatchAddResult{Container: "api", Kind: "pm2", Added: true}) {
		t.Errorf("add = %+v", added)
	}

	var again watch.WatchAddResult
	strictInto(t, runWatchJSON(t, newWatchAddCmd(), "--kind", "pm2", "api"), &again)
	if again.Added {
		t.Errorf("adding it twice = %+v, want added false", again)
	}

	var removed watch.WatchRemoveResult
	strictInto(t, runWatchJSON(t, newWatchRemoveCmd(), "api"), &removed)
	if removed != (watch.WatchRemoveResult{Container: "api", Removed: true}) {
		t.Errorf("remove = %+v", removed)
	}
}

// watch_add with a unit, pointed at a server, sends it as --unit, so the
// command has to keep it. Without it the target was watched under its display
// name, which for a systemd unit named differently is a unit that is not there.
func TestWatchAddKeepsTheUnit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOMEBUTLER_CONFIG", "")

	runWatchJSON(t, newWatchAddCmd(), "--kind", "systemd", "web", "--unit", "nginx.service")

	dir, err := watch.WatchDir()
	if err != nil {
		t.Fatal(err)
	}
	targets, err := watch.LoadTargets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Container != "web" || targets[0].Unit != "nginx.service" {
		t.Fatalf("targets = %+v, want web watched as nginx.service", targets)
	}
}

// The unit reaches the same commands the name does, so it is held to the same
// rule.
func TestWatchAddRefusesAUnitTheNameRuleRefuses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOMEBUTLER_CONFIG", "")
	// --unit=--help would reach systemctl as `systemctl restart --help`,
	// which exits 0; -- -web would reach docker as an option.
	for _, args := range [][]string{
		{"--kind", "systemd", "web", "--unit", "nginx service; rm"},
		{"--kind", "systemd", "web", "--unit=--help"},
		{"--kind", "docker", "--", "-web"},
	} {
		cmd := newWatchAddCmd()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Errorf("watch add %q was accepted", args)
		}
	}
	dir, _ := watch.WatchDir()
	if targets, _ := watch.LoadTargets(dir); len(targets) != 0 {
		t.Fatalf("a refused target was stored: %+v", targets)
	}
}
