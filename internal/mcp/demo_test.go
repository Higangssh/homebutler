package mcp

import (
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

func TestExecuteDemoTool_Basic(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)

	cases := []string{"system_status", "docker_list", "open_ports", "network_scan", "alerts"}
	for _, tool := range cases {
		res, err := s.executeDemoTool(tool, map[string]any{"server": "homelab-server"})
		if err != nil {
			t.Fatalf("tool %s failed: %v", tool, err)
		}
		if res == nil {
			t.Fatalf("tool %s returned nil", tool)
		}
	}
}

func TestExecuteDemoTool_RequiredArgs(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)

	if _, err := s.executeDemoTool("docker_restart", nil); err == nil {
		t.Fatal("expected error for missing docker_restart name")
	}
	if _, err := s.executeDemoTool("docker_stop", nil); err == nil {
		t.Fatal("expected error for missing docker_stop name")
	}
	if _, err := s.executeDemoTool("docker_logs", nil); err == nil {
		t.Fatal("expected error for missing docker_logs name")
	}
	if _, err := s.executeDemoTool("wake", nil); err == nil {
		t.Fatal("expected error for missing wake target")
	}
}

func TestExecuteDemoTool_Unknown(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)
	if _, err := s.executeDemoTool("unknown_tool", nil); err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestDemoLogsFallback(t *testing.T) {
	res := demoLogs("not-found")
	logs, ok := res["logs"].(string)
	if !ok || logs == "" {
		t.Fatal("expected fallback logs message")
	}
}

// Demo mode advertises the same tools/list as a real server, so a client that
// reads that list and calls one of them must not be told the tool does not
// exist. Six tools were advertised without a demo arm before this test:
// docker_stats and the five install_* tools.
//
// Arguments are filled from the schema's properties rather than only its
// required list, because a tool can need an argument the list cannot express:
// backup_drill takes app or all=true, so neither is Required and calling it
// with nothing fails.
func TestEveryAdvertisedToolHasADemoImplementation(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)

	sample := map[string]any{
		"name":    "nginx",
		"app":     "uptime-kuma",
		"target":  "aa:bb:cc:dd:ee:ff",
		"archive": "demo.tar.gz",
	}

	for _, c := range capabilityRegistry {
		args := map[string]any{}
		for prop := range c.tool.InputSchema.Properties {
			if v, ok := sample[prop]; ok {
				args[prop] = v
			}
		}
		// Every required argument has to be covered, or the tool is not really
		// being exercised.
		for _, req := range c.tool.InputSchema.Required {
			if _, ok := args[req]; !ok {
				t.Fatalf("tool %q requires argument %q; add a sample value to this test", c.tool.Name, req)
			}
		}

		if _, err := s.executeDemoTool(c.tool.Name, args); err != nil {
			t.Errorf("demo mode advertises %q but calling it fails: %v", c.tool.Name, err)
		}
	}
}
