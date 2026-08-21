package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Higangssh/homebutler/internal/ports"
)

// portExpect captures the fields the fixture tests assert on for a single port.
type portExpect struct {
	port    string
	process string
	pid     string
	address string
	public  bool
}

func TestParseLinuxSSFixture(t *testing.T) {
	out := mustReadFixture(t, "ss-linux.txt")
	parsed := ports.ParseLinuxOutput(string(out))

	if len(parsed) != 5 {
		t.Fatalf("expected 5 parsed ports from ss fixture, got %d", len(parsed))
	}

	expects := []portExpect{
		{port: "8080", process: "app-api", pid: "1001", address: "0.0.0.0", public: true},
		{port: "443", process: "demo-service", pid: "1002", address: "[::]", public: true},
		{port: "9090", process: "metrics-agent", pid: "1003", address: "127.0.0.1", public: false},
		{port: "8443", process: "edge-router", pid: "1004", address: "0.0.0.0", public: true},
		{port: "22", process: "ssh-relay", pid: "1005", address: "0.0.0.0", public: true},
	}

	for _, e := range expects {
		p, ok := findPortByNumber(parsed, e.port)
		if !ok {
			t.Errorf("port %s: not found in parsed output", e.port)
			continue
		}
		assertPort(t, e, p)
	}
}

func TestParseDarwinLsofFixture(t *testing.T) {
	out := mustReadFixture(t, "lsof-macos.txt")
	parsed := ports.ParseDarwinOutput(string(out))

	// 5 input lines, but the IPv4/IPv6 *:8080 pair for app-api is deduped to 1.
	if len(parsed) != 4 {
		t.Fatalf("expected 4 parsed ports from lsof fixture (after IPv4/IPv6 dedup), got %d", len(parsed))
	}

	expects := []portExpect{
		{port: "8080", process: "app-api", pid: "1001", address: "*", public: true},
		{port: "443", process: "demo-service", pid: "1002", address: "*", public: true},
		{port: "9090", process: "metrics-agent", pid: "1003", address: "127.0.0.1", public: false},
		{port: "8443", process: "edge-router", pid: "1004", address: "*", public: true},
	}

	for _, e := range expects {
		p, ok := findPortByNumber(parsed, e.port)
		if !ok {
			t.Errorf("port %s: not found in parsed output", e.port)
			continue
		}
		assertPort(t, e, p)
	}
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func findPortByNumber(list []ports.PortInfo, port string) (ports.PortInfo, bool) {
	for _, p := range list {
		if p.Port == port {
			return p, true
		}
	}
	return ports.PortInfo{}, false
}

func assertPort(t *testing.T, e portExpect, p ports.PortInfo) {
	t.Helper()
	if p.Protocol != "tcp" {
		t.Errorf("port %s: protocol = %q, want tcp", e.port, p.Protocol)
	}
	if p.Port != e.port {
		t.Errorf("port %s: port number = %q, want %q", e.port, p.Port, e.port)
	}
	if p.Process != e.process {
		t.Errorf("port %s: process = %q, want %q", e.port, p.Process, e.process)
	}
	if p.PID != e.pid {
		t.Errorf("port %s: pid = %q, want %q", e.port, p.PID, e.pid)
	}
	if p.Address != e.address {
		t.Errorf("port %s: address = %q, want %q", e.port, p.Address, e.address)
	}
	if got := ports.IsPublicBind(p.Address); got != e.public {
		t.Errorf("port %s: exposure public = %v, want %v (address %q)", e.port, got, e.public, p.Address)
	}
}
