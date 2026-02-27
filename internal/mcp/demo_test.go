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
