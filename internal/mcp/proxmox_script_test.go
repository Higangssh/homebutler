package mcp

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/proxmox"
)

func TestProxmoxScriptToolsAreLocalReadOnly(t *testing.T) {
	for _, name := range []string{"proxmox_script_list", "proxmox_script_command"} {
		cap, ok := capability.For(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if cap.Risk != capability.RiskRead || !cap.Supports(capability.TargetLocal) || cap.Supports(capability.TargetProxmox) || cap.Supports(capability.TargetServer) {
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
	command, ok := result.(ProxmoxScriptCommandResult)
	if !ok || command.Slug != "docker" || !strings.Contains(command.Command, "/ct/docker.sh") || command.Warning != proxmox.ScriptWarning {
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
	if m, ok := command.(ProxmoxScriptCommandResult); !ok || m.Slug != "docker" || m.Warning != proxmox.ScriptWarning {
		t.Fatalf("demo proxmox_script_command = %#v", command)
	}
}
