package ports

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitAddrPort(t *testing.T) {
	tests := []struct {
		in       string
		wantAddr string
		wantPort string
	}{
		{"127.0.0.1:8080", "127.0.0.1", "8080"},
		{"*:443", "*", "443"},
		{"[::1]:3000", "[::1]", "3000"},
		{":::22", "::", "22"},
		{"noport", "noport", ""},
		{"0.0.0.0:80", "0.0.0.0", "80"},
		{"[::]:8080", "[::]", "8080"},
	}
	for _, tc := range tests {
		a, p := splitAddrPort(tc.in)
		if a != tc.wantAddr || p != tc.wantPort {
			t.Fatalf("splitAddrPort(%q) = (%q,%q), want (%q,%q)", tc.in, a, p, tc.wantAddr, tc.wantPort)
		}
	}
}

func TestParseDarwinOutputEmpty(t *testing.T) {
	ports := parseDarwinOutput("")
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for empty input, got %d", len(ports))
	}
}

func TestParseDarwinOutputHeaderOnly(t *testing.T) {
	output := "COMMAND     PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME"
	ports := parseDarwinOutput(output)
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for header-only input, got %d", len(ports))
	}
}

func TestParseDarwinOutputDeduplication(t *testing.T) {
	output := `COMMAND     PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
nginx      5678   root     6u  IPv4 0x123456794      0t0  TCP *:80 (LISTEN)
nginx      5678   root     7u  IPv4 0x123456795      0t0  TCP *:80 (LISTEN)`

	ports := parseDarwinOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port (deduped), got %d", len(ports))
	}
}

func TestParseLinuxOutputMultipleProcesses(t *testing.T) {
	output := `State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process
LISTEN     0      128          0.0.0.0:22          0.0.0.0:*     users:(("sshd",pid=1234,fd=3),("sshd",pid=5678,fd=4))`

	ports := parseLinuxOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Process != "sshd" {
		t.Errorf("expected process sshd, got %s", ports[0].Process)
	}
	if ports[0].PID != "1234" {
		t.Errorf("expected first pid 1234, got %s", ports[0].PID)
	}
}

func TestParseLinuxOutputEmpty(t *testing.T) {
	ports := parseLinuxOutput("")
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for empty input, got %d", len(ports))
	}
}

func TestParseLinuxOutputHeaderOnly(t *testing.T) {
	output := "State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process"
	ports := parseLinuxOutput(output)
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for header-only input, got %d", len(ports))
	}
}

func TestParseLinuxOutputNoProcessInfo(t *testing.T) {
	output := `State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process
LISTEN     0      128          0.0.0.0:22          0.0.0.0:*`

	ports := parseLinuxOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Process != "" {
		t.Errorf("expected empty process, got %s", ports[0].Process)
	}
	if ports[0].PID != "" {
		t.Errorf("expected empty pid for unprivileged output, got %s", ports[0].PID)
	}
	if ports[0].Port != "22" {
		t.Errorf("expected port 22, got %s", ports[0].Port)
	}
}

func TestParseLinuxOutputIPv6(t *testing.T) {
	output := `State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process
LISTEN     0      128             [::]:80              [::]:*     users:(("apache2",pid=999,fd=4))`

	ports := parseLinuxOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Address != "[::]" {
		t.Errorf("expected address [::], got %s", ports[0].Address)
	}
	if ports[0].Process != "apache2" {
		t.Errorf("expected process apache2, got %s", ports[0].Process)
	}
	if ports[0].PID != "999" {
		t.Errorf("expected pid 999, got %s", ports[0].PID)
	}
}

func TestParseLinuxSSFixture(t *testing.T) {
	out := mustReadFixture(t, "ss-linux.txt")
	parsed := parseLinuxOutput(string(out))

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
	parsed := parseDarwinOutput(string(out))

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

func TestPortInfoStruct(t *testing.T) {
	p := PortInfo{
		Protocol: "tcp",
		Address:  "0.0.0.0",
		Port:     "8080",
		PID:      "1234",
		Process:  "myapp",
	}
	if p.Protocol != "tcp" {
		t.Errorf("Protocol = %q, want tcp", p.Protocol)
	}
	if p.Port != "8080" {
		t.Errorf("Port = %q, want 8080", p.Port)
	}
}

func TestIsPublicBind(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"lsof wildcard", "*", true},
		{"ipv4 wildcard", "0.0.0.0", true},
		{"ipv6 wildcard", "::", true},
		{"ipv6 wildcard bracketed", "[::]", true},
		{"empty address parses as wildcard", "", true},
		{"ipv4 loopback", "127.0.0.1", false},
		{"ipv6 loopback", "::1", false},
		{"ipv6 loopback bracketed", "[::1]", false},
		{"routable address", "172.31.11.103", false},
		{"routable ipv6 bracketed", "[2001:db8::1]", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPublicBind(tc.addr); got != tc.want {
				t.Errorf("IsPublicBind(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestIsPublicBindMatchesSplitAddrPortOutput(t *testing.T) {
	tests := []struct {
		local string
		want  bool
	}{
		{"0.0.0.0:22", true},
		{"[::]:22", true},
		{"*:8080", true},
		{"127.0.0.1:8080", false},
		{"[::1]:3000", false},
		{":::22", true},
	}
	for _, tc := range tests {
		addr, _ := splitAddrPort(tc.local)
		if got := IsPublicBind(addr); got != tc.want {
			t.Errorf("%q -> addr %q: IsPublicBind = %v, want %v", tc.local, addr, got, tc.want)
		}
	}
}

type portExpect struct {
	port    string
	process string
	pid     string
	address string
	public  bool
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func findPortByNumber(list []PortInfo, port string) (PortInfo, bool) {
	for _, p := range list {
		if p.Port == port {
			return p, true
		}
	}
	return PortInfo{}, false
}

func assertPort(t *testing.T, e portExpect, p PortInfo) {
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
	if got := IsPublicBind(p.Address); got != e.public {
		t.Errorf("port %s: exposure public = %v, want %v (address %q)", e.port, got, e.public, p.Address)
	}
}
