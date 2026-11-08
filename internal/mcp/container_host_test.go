package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/system"
)

// containerServer is an MCP server that believes it runs in a container, with
// a local server and a remote one configured, as the published image would be
// with a config mounted.
func containerServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOMEBUTLER_CONTAINER", "1")
	s, out := newTestServer()
	s.cfg.Servers = []config.ServerConfig{
		{Name: "mini", Host: "192.0.2.10", Local: true},
		{Name: "pi", Host: "127.0.0.1", Port: 1, KeyFile: "/nonexistent/homebutler-test-key"},
	}
	return s, out
}

// Inside a container the machine this binary runs on is the container, so a
// local answer from these tools describes a machine nobody asked about. It has
// to be refused whether the call left server out or named a local: true one,
// because the second is the case that also labels the numbers with a real
// machine's name.
func TestContainerRefusesLocalHostReadings(t *testing.T) {
	s, _ := containerServer(t)
	for name := range hostDescribingTools {
		for _, server := range []string{"", "mini"} {
			args := map[string]any{}
			if server != "" {
				args["server"] = server
			}
			_, err := s.executeTool(name, args)
			if !errors.Is(err, system.ErrContainerCannotSeeHost) {
				t.Errorf("%s (server=%q) in a container: err = %v, want ErrContainerCannotSeeHost", name, server, err)
			}
		}
	}
}

// What an agent reads is the sentence serve gives, which names both ways out,
// not the Go error value.
func TestContainerRefusalReadsLikeServe(t *testing.T) {
	s, out := containerServer(t)
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system_status","arguments":{}}}`)
	raw, _ := json.Marshal(resp.Result)
	var result toolsCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.IsError {
		t.Fatalf("system_status in a container answered: %s", raw)
	}
	if len(result.Content) != 1 || result.Content[0].Text != system.ContainerCannotSeeHost {
		t.Errorf("refusal text = %q, want system.ContainerCannotSeeHost", result.Content)
	}
}

// A remote server is the way out the refusal names, so it has to keep
// working: the call must reach SSH, which here fails on the missing key.
func TestContainerStillReachesRemoteServers(t *testing.T) {
	s, _ := containerServer(t)
	for name := range hostDescribingTools {
		c, _ := capability.For(name)
		if !c.Supports(capability.TargetServer) {
			continue
		}
		// format=json because mermaid is a local-only rendering.
		_, err := s.executeTool(name, map[string]any{"server": "pi", "format": "json"})
		if errors.Is(err, system.ErrContainerCannotSeeHost) {
			t.Errorf("%s pointed at a remote server was refused as if it were local", name)
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "failed to load SSH key") {
			t.Errorf("%s pointed at a remote server: err = %v, want the SSH key failure", name, err)
		}
	}
}

// Tools that do not describe the machine stay available. config_validate
// reads the mounted file and proxmox_script_list is a static catalog.
func TestContainerLeavesOtherToolsAlone(t *testing.T) {
	s, _ := containerServer(t)
	for _, name := range []string{"config_validate", "proxmox_script_list", "install_list"} {
		if _, err := s.executeTool(name, map[string]any{}); errors.Is(err, system.ErrContainerCannotSeeHost) {
			t.Errorf("%s was refused in a container, and it does not describe the host", name)
		}
	}
}

// Every name in the set is a real tool that can run locally, so a rename
// cannot quietly drop a tool out of the guard.
func TestHostDescribingToolsAreLocalTools(t *testing.T) {
	for name := range hostDescribingTools {
		c, ok := capability.For(name)
		if !ok {
			t.Errorf("%q is not in the capability registry", name)
			continue
		}
		if !c.Supports(capability.TargetLocal) {
			t.Errorf("%q does not run locally, so guarding its local path guards nothing", name)
		}
	}
}
