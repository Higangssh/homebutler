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
