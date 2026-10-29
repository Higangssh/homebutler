package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDockerTop answers `docker top <c> -o …` with the comm fixture and plain
// `docker top <c>` with the other, both from the docker package's testdata.
func fakeDockerTop(t *testing.T, commFixture, plainFixture string) {
	t.Helper()
	path := func(name string) string {
		abs, err := filepath.Abs(filepath.Join("..", "docker", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return abs
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = -o ]; then cat '" + path(commFixture) + "'; exit 0; fi\ndone\ncat '" + path(plainFixture) + "'\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// docker_top is a read tool, offered under --max-risk read. An agent that is
// only allowed to look must not be handed a token from a container's command
// line: not as a flag value, not as a positional argument, and not as
// argv[0], which the process sets for itself.
func TestDockerTopToolLeavesOutArguments(t *testing.T) {
	cases := []struct {
		name, comm, plain string
		secrets           []string
	}{
		{"flag and positional", "top-secret-args-comm.txt", "top-secret-args.txt",
			[]string{"FAKE123", "hunter2-positional", "--token"}},
		{"argv0", "top-argv0-comm.txt", "top-argv0.txt",
			[]string{"XA1", "HOSTB2", "SECRETC3", "XD4", "XE5", "3601", "--token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeDockerTop(t, tc.comm, tc.plain)
			s, out := newTestServer()
			resp := sendAndReceive(t, s, out,
				`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"docker_top","arguments":{"name":"probe"}}}`)
			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
			raw := out.String()
			if !strings.Contains(raw, `\"command\":\"sh`) && !strings.Contains(raw, `\"command\":\"bash`) {
				t.Fatalf("the tool result does not name the processes: %s", raw)
			}
			for _, secret := range tc.secrets {
				if strings.Contains(raw, secret) {
					t.Errorf("docker_top result carries %q: %s", secret, raw)
				}
			}
		})
	}
}

// A remote homebutler older than this rule answers with full command lines.
// The MCP server is what made the promise to the agent, so it narrows them
// on the way through.
func TestRemoteDockerTopNarrowsAnOlderAnswer(t *testing.T) {
	old := `{"container":"probe","processes":[{"pid":"4127","user":"root","command":"/usr/local/bin/probe serve --token=FAKE123 hunter2-positional"}]}`
	result, err := remoteDockerTop([]byte(old))
	if err != nil {
		t.Fatalf("remoteDockerTop: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"container":"probe","processes":[{"pid":"4127","user":"root","command":"probe"}]}`; string(data) != want {
		t.Errorf("got %s, want %s", data, want)
	}
	if _, err := remoteDockerTop([]byte("not json")); err == nil {
		t.Error("a non-JSON remote answer must be an error")
	}
}
