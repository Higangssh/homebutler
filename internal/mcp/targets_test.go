package mcp

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// Every registry entry declares at least targetLocal. A tool that can be
// pointed nowhere is registered but unreachable, which is a mistake rather than
// a configuration.
func TestEveryCapabilityDeclaresATarget(t *testing.T) {
	for _, c := range capabilityRegistry {
		if len(c.targets) == 0 {
			t.Errorf("tool %q declares no targets", c.tool.Name)
			continue
		}
		if !c.supports(targetLocal) {
			t.Errorf("tool %q does not support targetLocal; every tool runs on this machine", c.tool.Name)
		}
		for _, k := range c.targets {
			switch k {
			case targetLocal, targetServer:
			default:
				t.Errorf("tool %q declares unknown target %q", c.tool.Name, k)
			}
		}
	}
}

// The registry now gates remote routing, so what it claims and what
// executeRemote can actually build have to be the same set. They agreed by
// coincidence before this change; this is what makes that structural.
//
// Reading the source rather than calling executeRemote keeps the check free of
// an SSH connection: the argv mapping is a compile-time switch, so its cases
// are the complete list of tools that have a remote command.
func TestRegistryTargetsMatchRemoteArgvMapping(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("reading server.go: %v", err)
	}

	fn := regexp.MustCompile(`(?s)func \(s \*Server\) executeRemote\(.*?\n\tout, err := remote\.Run`)
	body := fn.Find(src)
	if body == nil {
		t.Fatal("could not locate the executeRemote argv switch; update this test if it moved")
	}

	mapped := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([a-z_]+)":`).FindAllSubmatch(body, -1) {
		mapped[string(m[1])] = true
	}
	if len(mapped) == 0 {
		t.Fatal("found no case arms in executeRemote; the regex above needs updating")
	}

	for _, c := range capabilityRegistry {
		claims := c.supports(targetServer)
		switch {
		case claims && !mapped[c.tool.Name]:
			t.Errorf("%q declares targetServer but executeRemote has no argv mapping for it, so pointing it at a server fails after the gate lets it through", c.tool.Name)
		case !claims && mapped[c.tool.Name]:
			t.Errorf("%q has an argv mapping in executeRemote but does not declare targetServer, so the gate now rejects it and the mapping is dead", c.tool.Name)
		}
	}

	for name := range mapped {
		if _, ok := capabilityFor(name); !ok {
			t.Errorf("executeRemote maps %q, which is not in the capability registry", name)
		}
	}
}

func TestCapabilityForUnknownTool(t *testing.T) {
	if _, ok := capabilityFor("no_such_tool"); ok {
		t.Error("capabilityFor returned a capability for an unregistered tool")
	}
}

// The gate has to reject before routing, not after. network_scan reads the
// local network and has no remote path; pointing it at a server must fail on
// the registry rather than travelling to executeRemote and falling through the
// argv switch, which is where the decision used to live.
func TestPointingALocalOnlyToolAtAServerIsRejected(t *testing.T) {
	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "pve1", Host: "192.0.2.1", Local: false},
		},
	}
	s := NewServer(cfg, "test")

	_, err := s.executeTool("network_scan", map[string]any{"server": "pve1"})
	if err == nil {
		t.Fatal("expected network_scan pointed at a remote server to be rejected")
	}
	if !strings.Contains(err.Error(), "cannot be pointed at a server") {
		t.Errorf("rejection should name the reason, got: %v", err)
	}

	// The rejection must come from the registry, not from a failed SSH attempt.
	if strings.Contains(err.Error(), "no remote command mapping") {
		t.Error("rejected inside executeRemote; the gate did not run first")
	}
}

func TestUnknownServerStillReportedBeforeTheGate(t *testing.T) {
	s := NewServer(&config.Config{}, "test")
	_, err := s.executeTool("docker_list", map[string]any{"server": "nope"})
	if err == nil || !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("an unknown server should be reported as such, got: %v", err)
	}
}
