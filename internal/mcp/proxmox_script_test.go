package mcp

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/proxmox"
)

func TestProxmoxScriptToolsAreLocalReadOnly(t *testing.T) {
	for _, name := range []string{"proxmox_script_list", "proxmox_script_command"} {
		cap, ok := capabilityFor(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if cap.risk != riskRead || !cap.supports(targetLocal) || cap.supports(targetProxmox) || cap.supports(targetServer) {
			t.Errorf("%s capability = %#v", name, cap)
		}
	}
}

func TestProxmoxScriptListTool(t *testing.T) {
	s := NewServer(&config.Config{}, "test")
	result, err := s.executeTool("proxmox_script_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	scripts, ok := result.([]proxmox.Script)
	if !ok || len(scripts) == 0 {
		t.Fatalf("proxmox_script_list = %#v", result)
	}
}

func TestProxmoxScriptCommandTool(t *testing.T) {
	s := NewServer(&config.Config{}, "test")
	result, err := s.executeTool("proxmox_script_command", map[string]any{"slug": "docker"})
	if err != nil {
		t.Fatal(err)
	}
	command, ok := result.(map[string]any)
	if !ok || command["slug"] != "docker" || !strings.Contains(command["command"].(string), "/ct/docker.sh") || command["warning"] != proxmox.ScriptWarning {
		t.Fatalf("proxmox_script_command = %#v", result)
	}

	if _, err := s.executeTool("proxmox_script_command", map[string]any{}); err == nil || !strings.Contains(err.Error(), "missing required parameter: slug") {
		t.Errorf("missing slug error = %v", err)
	}
	if _, err := s.executeTool("proxmox_script_command", map[string]any{"slug": "nope"}); err == nil || !strings.Contains(err.Error(), "unknown Proxmox Community Script") {
		t.Errorf("unknown slug error = %v", err)
	}
}

func TestProxmoxScriptToolsDemo(t *testing.T) {
	s := NewServer(&config.Config{}, "test", true)
	list, err := s.executeDemoTool("proxmox_script_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if scripts, ok := list.([]proxmox.Script); !ok || len(scripts) == 0 {
		t.Fatalf("demo proxmox_script_list = %#v", list)
	}

	command, err := s.executeDemoTool("proxmox_script_command", map[string]any{"slug": "docker"})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := command.(map[string]any); !ok || m["slug"] != "docker" || m["warning"] != proxmox.ScriptWarning {
		t.Fatalf("demo proxmox_script_command = %#v", command)
	}
}
