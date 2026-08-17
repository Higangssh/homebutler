package ports

import "testing"

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

func TestParseDarwinOutput(t *testing.T) {
	output := `COMMAND     PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
rapportd    427 sanghee    3u  IPv4 0x123456789      0t0  TCP *:49152 (LISTEN)
rapportd    427 sanghee    4u  IPv6 0x123456790      0t0  TCP *:49152 (LISTEN)
ControlCe   488 sanghee    9u  IPv4 0x123456791      0t0  TCP *:7000 (LISTEN)
ControlCe   488 sanghee   10u  IPv6 0x123456792      0t0  TCP *:7000 (LISTEN)
node       1234 sanghee   22u  IPv4 0x123456793      0t0  TCP 127.0.0.1:3000 (LISTEN)
nginx      5678   root     6u  IPv4 0x123456794      0t0  TCP *:80 (LISTEN)
nginx      5678   root     7u  IPv4 0x123456795      0t0  TCP *:443 (LISTEN)`

	ports := ParseDarwinOutput(output)

	if len(ports) != 5 {
		t.Fatalf("expected 5 ports (deduped), got %d", len(ports))
	}

	// Check first entry
	if ports[0].Process != "rapportd" {
		t.Errorf("expected process rapportd, got %s", ports[0].Process)
	}
	if ports[0].PID != "427" {
		t.Errorf("expected PID 427, got %s", ports[0].PID)
	}
	if ports[0].Port != "49152" {
		t.Errorf("expected port 49152, got %s", ports[0].Port)
	}
	if ports[0].Protocol != "tcp" {
		t.Errorf("expected protocol tcp, got %s", ports[0].Protocol)
	}

	// Check node on localhost (index 2: rapportd, ControlCe, node, nginx:80, nginx:443)
	if ports[2].Address != "127.0.0.1" {
		t.Errorf("expected address 127.0.0.1, got %s", ports[2].Address)
	}
	if ports[2].Port != "3000" {
		t.Errorf("expected port 3000, got %s", ports[2].Port)
	}
}

func TestParseDarwinOutput_Empty(t *testing.T) {
	ports := ParseDarwinOutput("")
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for empty input, got %d", len(ports))
	}
}

func TestParseDarwinOutput_HeaderOnly(t *testing.T) {
	output := "COMMAND     PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME"
	ports := ParseDarwinOutput(output)
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for header-only input, got %d", len(ports))
	}
}

func TestParseDarwinOutput_Deduplication(t *testing.T) {
	// Same process, same address:port should be deduped
	output := `COMMAND     PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
nginx      5678   root     6u  IPv4 0x123456794      0t0  TCP *:80 (LISTEN)
nginx      5678   root     7u  IPv4 0x123456795      0t0  TCP *:80 (LISTEN)`

	ports := ParseDarwinOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port (deduped), got %d", len(ports))
	}
}

func TestParseLinuxOutput(t *testing.T) {
	output := `State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process
LISTEN     0      128          0.0.0.0:22          0.0.0.0:*     users:(("sshd",pid=1234,fd=3))
LISTEN     0      511          0.0.0.0:80          0.0.0.0:*     users:(("nginx",pid=5678,fd=6))
LISTEN     0      128        127.0.0.1:3000        0.0.0.0:*     users:(("node",pid=9012,fd=22))
LISTEN     0      128             [::]:443            [::]:*     users:(("nginx",pid=5678,fd=7))`

	ports := ParseLinuxOutput(output)

	if len(ports) != 4 {
		t.Fatalf("expected 4 ports, got %d", len(ports))
	}

	// Check sshd
	if ports[0].Port != "22" {
		t.Errorf("expected port 22, got %s", ports[0].Port)
	}
	if ports[0].Process != "sshd" {
		t.Errorf("expected process sshd, got %s", ports[0].Process)
	}
	if ports[0].Address != "0.0.0.0" {
		t.Errorf("expected address 0.0.0.0, got %s", ports[0].Address)
	}

	// Check node on localhost
	if ports[2].Address != "127.0.0.1" {
		t.Errorf("expected address 127.0.0.1, got %s", ports[2].Address)
	}
	if ports[2].Process != "node" {
		t.Errorf("expected process node, got %s", ports[2].Process)
	}
}

func TestParseLinuxOutput_Empty(t *testing.T) {
	ports := ParseLinuxOutput("")
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for empty input, got %d", len(ports))
	}
}

func TestParseLinuxOutput_HeaderOnly(t *testing.T) {
	output := "State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process"
	ports := ParseLinuxOutput(output)
	if len(ports) != 0 {
		t.Fatalf("expected 0 ports for header-only input, got %d", len(ports))
	}
}

func TestParseLinuxOutput_NoProcessInfo(t *testing.T) {
	output := `State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process
LISTEN     0      128          0.0.0.0:22          0.0.0.0:*`

	ports := ParseLinuxOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Process != "" {
		t.Errorf("expected empty process, got %s", ports[0].Process)
	}
	if ports[0].Port != "22" {
		t.Errorf("expected port 22, got %s", ports[0].Port)
	}
}

func TestParseLinuxOutput_IPv6(t *testing.T) {
	output := `State      Recv-Q Send-Q Local Address:Port   Peer Address:Port Process
LISTEN     0      128             [::]:80              [::]:*     users:(("apache2",pid=999,fd=4))`

	ports := ParseLinuxOutput(output)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Address != "[::]" {
		t.Errorf("expected address [::], got %s", ports[0].Address)
	}
	if ports[0].Process != "apache2" {
		t.Errorf("expected process apache2, got %s", ports[0].Process)
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
