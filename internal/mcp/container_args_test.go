package mcp

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

func rejectionOf(t *testing.T, s *Server, tool string, args map[string]any) string {
	t.Helper()
	_, err := s.executeTool(tool, args)
	if err == nil {
		t.Fatalf("%s with %v was accepted; expected a rejection", tool, args)
	}
	return err.Error()
}

// A flag-shaped name used to reach the remote homebutler as a bare argv
// element whose cobra parser turned it into a flag: docker_restart came back
// with help text and exit zero, reading as a restart that never happened.
// The gate in executeTool refuses it before routing, so the answer is the
// same words whichever machine would have answered — and no SSH connection
// is ever attempted for an input that cannot be executed.
func TestFlagShapedNameRefusedWithSameWordsOnBothPaths(t *testing.T) {
	s := NewServer(&config.Config{
		Servers: []config.ServerConfig{{Name: "pve1", Host: "192.0.2.1"}},
	}, "test")

	for _, tool := range []string{"docker_restart", "docker_stop", "docker_logs", "docker_top", "docker_inspect"} {
		local := rejectionOf(t, s, tool, map[string]any{"name": "--help"})
		remote := rejectionOf(t, s, tool, map[string]any{"server": "pve1", "name": "--help"})

		want := "invalid container name: --help"
		if local != want {
			t.Errorf("%s local rejection = %q, want %q", tool, local, want)
		}
		if remote != local {
			t.Errorf("%s pointed at a server answers %q, locally %q; the paths disagree", tool, remote, local)
		}
		// The rejection must come from the gate rather than a failed dial: on
		// this config any real attempt would surface a connection error, and
		// before the fix it surfaced success carrying help text instead.
		if strings.Contains(remote, "ssh") || strings.Contains(remote, "connection") {
			t.Errorf("%s reached the remote host before being rejected: %q", tool, remote)
		}
	}
}

// Remotely, a missing name used to travel as an empty argument and come back
// as whatever the far side makes of it; locally it was refused outright.
func TestMissingNameRejectedOnBothPaths(t *testing.T) {
	s := NewServer(&config.Config{
		Servers: []config.ServerConfig{{Name: "pve1", Host: "192.0.2.1"}},
	}, "test")

	for _, tool := range []string{"docker_restart", "docker_stop", "docker_logs", "docker_top", "docker_inspect"} {
		local := rejectionOf(t, s, tool, map[string]any{})
		remote := rejectionOf(t, s, tool, map[string]any{"server": "pve1"})

		want := "missing required parameter: name"
		if local != want || remote != want {
			t.Errorf("%s missing name: local %q, remote %q, want %q", tool, local, remote, want)
		}
	}
}

// lines rides next to the name and had the same shape: "--json" became a
// flag of the remote command rather than the bad number it is.
func TestDockerLogsLinesRejectedBeforeRouting(t *testing.T) {
	s := NewServer(&config.Config{
		Servers: []config.ServerConfig{{Name: "pve1", Host: "192.0.2.1"}},
	}, "test")

	for _, lines := range []string{"--json", "-n", "ten", "10x"} {
		local := rejectionOf(t, s, "docker_logs", map[string]any{"name": "web", "lines": lines})
		remote := rejectionOf(t, s, "docker_logs", map[string]any{"server": "pve1", "name": "web", "lines": lines})

		want := fmt.Sprintf("invalid line count: %s (must be a positive integer)", lines)
		if local != want || remote != want {
			t.Errorf("lines %q: local %q, remote %q, want %q", lines, local, remote, want)
		}
	}
}

// The gate itself, without routing: acceptance must hold for every forwarded
// tool, and tools that forward nothing must pass through untouched.
func TestValidateContainerArgs(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    map[string]any
		wantErr string
	}{
		{"restart", "docker_restart", map[string]any{"name": "web"}, ""},
		{"stop dotted name", "docker_stop", map[string]any{"name": "app.v2_1"}, ""},
		{"logs default lines", "docker_logs", map[string]any{"name": "web"}, ""},
		{"logs explicit lines", "docker_logs", map[string]any{"name": "web", "lines": "100"}, ""},
		{"logs empty lines falls back to default", "docker_logs", map[string]any{"name": "web", "lines": ""}, ""},
		{"top", "docker_top", map[string]any{"name": "web"}, ""},
		{"inspect", "docker_inspect", map[string]any{"name": "web"}, ""},
		{"missing name", "docker_restart", map[string]any{}, "missing required parameter: name"},
		{"empty name counts as missing", "docker_stop", map[string]any{"name": ""}, "missing required parameter: name"},
		{"long dash", "docker_restart", map[string]any{"name": "--rm"}, "invalid container name: --rm"},
		{"short dash", "docker_top", map[string]any{"name": "-H"}, "invalid container name: -H"},
		{"charset still applies", "docker_inspect", map[string]any{"name": "web;id"}, "invalid container name: web;id"},
		{"non numeric lines", "docker_logs", map[string]any{"name": "web", "lines": "ten"}, "invalid line count: ten (must be a positive integer)"},
		{"flag shaped lines", "docker_logs", map[string]any{"name": "web", "lines": "--json"}, "invalid line count: --json (must be a positive integer)"},
		{"tool forwarding nothing passes through", "network_scan", map[string]any{"name": "--help"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContainerArgs(tt.tool, tt.args)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tt.wantErr {
				t.Errorf("validateContainerArgs(%q, %v) = %q, want %q", tt.tool, tt.args, got, tt.wantErr)
			}
		})
	}
}

// Every executeRemote arm that forwards stringArg(args, "name") has to appear
// in containerArgTools, or a future tool reintroduces the gap where a
// flag-shaped value travels to the remote parser unchecked. Reading the source
// keeps this free of an SSH connection, the same reasoning as the registry
// test that pins the argv switch against the capability list.
func TestForwardedContainerNamesHaveAGate(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("reading server.go: %v", err)
	}

	fn := regexp.MustCompile(`(?s)func \(s \*Server\) executeRemote\(.*?\n\tout, err := remote\.Run`)
	body := fn.Find(src)
	if body == nil {
		t.Fatal("could not locate the executeRemote argv switch; update this test if it moved")
	}

	forwarded := map[string]bool{}
	for _, arm := range strings.Split(string(body), "\n\tcase ")[1:] {
		end := strings.Index(arm, `":`)
		if end < 0 {
			continue
		}
		if strings.Contains(arm, `stringArg(args, "name")`) {
			forwarded[strings.Trim(arm[:end], `"`)] = true
		}
	}
	if len(forwarded) == 0 {
		t.Fatal("found no arm forwarding a name in executeRemote; the split above needs updating")
	}

	for tool := range forwarded {
		if _, gated := containerArgTools[tool]; !gated {
			t.Errorf("executeRemote forwards name for %q but the gate does not cover it; add it to containerArgTools", tool)
		}
	}
	for tool := range containerArgTools {
		if !forwarded[tool] {
			t.Errorf("%q sits in containerArgTools but its executeRemote arm no longer forwards a name; drop it from the map", tool)
		}
	}
}
