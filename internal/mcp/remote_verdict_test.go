package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

// exitingRemote answers as a remote that printed stdout and then exited 1
// with reason on stderr.
func exitingRemote(t *testing.T, stdout, reason string) {
	t.Helper()
	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	runRemote = func(_ *config.ServerConfig, _ ...string) ([]byte, error) {
		return nil, &remote.ExitError{
			Status: 1,
			Stdout: []byte(stdout),
			Stderr: []byte(reason),
			Err:    errors.New("[pi] remote command failed: Process exited with status 1\n  → Output: " + reason),
		}
	}
}

// A drill that fails and a channel that does not send are answers, and the
// local tool returns them as answers. Over SSH the remote's exit status made
// them an error, so the agent never saw which step broke or which channel.
func TestAFailingVerdictFromAServerIsTheAnswer(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		out  string
	}{
		{"backup_drill", map[string]any{"app": "web"}, `{"app":"web","archive":"","passed":false,"error":"no backup found"}`},
		{"backup_drill", map[string]any{"all": true}, `{"results":[{"app":"web","passed":false}],"total":1,"passed":0,"failed":1}`},
		{"notify_test", nil, `[{"channel":"webhook","sent":false,"error":"dial tcp: connection refused"},{"channel":"ntfy","sent":true}]`},
	}
	for _, tc := range cases {
		exitingRemote(t, tc.out+"\n", "error: it failed\n")
		args := map[string]any{"server": "pi"}
		for k, v := range tc.args {
			args[k] = v
		}
		result, err := remoteServer(t).executeTool(tc.tool, args)
		if err != nil {
			t.Errorf("%s %v: %v, want the answer", tc.tool, tc.args, err)
			continue
		}
		data, _ := json.Marshal(result)
		var want any
		_ = json.Unmarshal([]byte(tc.out), &want)
		if wantJSON, _ := json.Marshal(want); string(wantJSON) != string(data) {
			t.Errorf("%s answered %s, want %s", tc.tool, data, wantJSON)
		}
	}
}

// The exit is believed over a document that disagrees with it, and every
// other tool's failing exit stays an error whatever it printed.
func TestOtherFailingExitsStayErrors(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		out  string
	}{
		// Passed beside a failing exit is not the verdict the exit reports.
		{"backup_drill", map[string]any{"app": "web"}, `{"app":"web","passed":true}`},
		{"backup_drill", map[string]any{"all": true}, `{"results":[],"total":0,"passed":0,"failed":0}`},
		{"notify_test", nil, `[{"channel":"ntfy","sent":true}]`},
		{"notify_test", nil, `{"error":"no notification channel"}`},
		// Not JSON at all: the reason is what there is.
		{"backup_drill", map[string]any{"app": "web"}, "Drill: web\n  failed\n"},
		{"backup_drill", map[string]any{"app": "web"}, ""},
		// Not a verdict tool, even with a document on stdout.
		{"doctor", nil, `{"schema_version":1,"status":"warn","summary":{"pass":0,"warn":1,"fail":0},"findings":[]}`},
		{"report", nil, `{"schema_version":1}`},
	}
	for _, tc := range cases {
		exitingRemote(t, tc.out, "error: it failed\n")
		args := map[string]any{"server": "pi"}
		for k, v := range tc.args {
			args[k] = v
		}
		_, err := remoteServer(t).executeTool(tc.tool, args)
		if err == nil {
			t.Errorf("%s %v with %q and a failing exit answered as if it succeeded", tc.tool, tc.args, tc.out)
			continue
		}
		if !strings.Contains(err.Error(), "it failed") {
			t.Errorf("%s: the remote's stderr is not in the error: %v", tc.tool, err)
		}
	}
}

// A connection that failed is never an answer.
func TestAConnectionFailureIsNotAnAnswer(t *testing.T) {
	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	runRemote = func(_ *config.ServerConfig, _ ...string) ([]byte, error) {
		return nil, errors.New("[pi] cannot connect: connection refused")
	}
	if _, err := remoteServer(t).executeTool("notify_test", map[string]any{"server": "pi"}); err == nil {
		t.Fatal("a connection failure answered")
	}
}
