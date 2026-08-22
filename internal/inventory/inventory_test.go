package inventory

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

func fakeFuncs(containers []docker.Container, portList []ports.PortInfo, dockerErr, portsErr error) CollectFuncs {
	return CollectFuncs{
		StatusFn: func() (*system.StatusInfo, error) {
			return &system.StatusInfo{
				Hostname: "testhost",
				OS:       "linux",
				Arch:     "amd64",
				Uptime:   "2d",
				CPU:      system.CPUInfo{UsagePercent: 25, Cores: 4},
				Memory:   system.MemInfo{TotalGB: 16, UsedGB: 8, Percent: 50},
			}, nil
		},
		DockerListFn: func() ([]docker.Container, error) {
			if dockerErr != nil {
				return nil, dockerErr
			}
			return containers, nil
		},
		PortsListFn: func() (*ports.Result, error) {
			if portsErr != nil {
				return nil, portsErr
			}
			return &ports.Result{Ports: portList}, nil
		},
	}
}

func TestCollect_Basic(t *testing.T) {
	containers := []docker.Container{
		{ID: "abc", Name: "nginx", Image: "nginx:latest", State: "running", Ports: "80/tcp"},
	}
	portList := []ports.PortInfo{
		{Protocol: "tcp", Port: "8080", Process: "go"},
	}

	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "myserver", Host: "192.168.1.10", Local: true},
		},
	}

	inv, err := Collect(cfg, fakeFuncs(containers, portList, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.ServerName != "myserver" {
		t.Errorf("expected server name 'myserver', got %q", inv.ServerName)
	}
	if inv.Host != "192.168.1.10" {
		t.Errorf("expected host '192.168.1.10', got %q", inv.Host)
	}
	if len(inv.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(inv.Containers))
	}
	if len(inv.Ports) != 1 {
		t.Errorf("expected 1 port, got %d", len(inv.Ports))
	}
	if len(inv.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", inv.Warnings)
	}
}

func TestCollect_DockerFailure(t *testing.T) {
	cfg := &config.Config{}
	fns := fakeFuncs(nil, nil, errTest("docker down"), nil)

	inv, err := Collect(cfg, fns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "docker down") {
		t.Errorf("expected docker warning, got %v", inv.Warnings)
	}
	if len(inv.Containers) != 0 {
		t.Errorf("expected empty containers on failure")
	}
}

func TestCollect_PortsFailure(t *testing.T) {
	cfg := &config.Config{}
	fns := fakeFuncs(nil, nil, nil, errTest("no lsof"))

	inv, err := Collect(cfg, fns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "no lsof") {
		t.Errorf("expected ports warning, got %v", inv.Warnings)
	}
}

func TestCollect_NoConfig(t *testing.T) {
	inv, err := Collect(nil, fakeFuncs(nil, nil, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to hostname.
	if inv.ServerName == "" {
		t.Error("expected non-empty server name from hostname fallback")
	}
}

func TestRenderTree(t *testing.T) {
	inv := &Inventory{
		ServerName: "lab",
		Host:       "10.0.0.1",
		System: &system.StatusInfo{
			OS: "linux", Arch: "amd64",
			CPU:    system.CPUInfo{UsagePercent: 10},
			Memory: system.MemInfo{TotalGB: 32, UsedGB: 16},
			Uptime: "5d",
		},
		Containers: []docker.Container{
			{Name: "web", State: "running", Image: "nginx:1"},
		},
		Ports: []ports.PortInfo{
			{Port: "443", Protocol: "tcp", Process: "nginx"},
		},
		Warnings: []string{"test warning"},
	}

	out := RenderTree(inv)

	for _, want := range []string{"Home Network", "lab", "10.0.0.1", "web", "running", "443/tcp", "test warning"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output missing %q\n%s", want, out)
		}
	}
}

func TestRenderMermaid(t *testing.T) {
	inv := &Inventory{
		ServerName: "lab",
		Host:       "10.0.0.1",
		System:     &system.StatusInfo{},
		Containers: []docker.Container{
			{Name: "web", State: "running"},
		},
		Ports: []ports.PortInfo{
			{Port: "80", Protocol: "tcp", Process: "nginx"},
		},
	}

	out := RenderMermaid(inv)

	if !strings.HasPrefix(out, "graph TD\n") {
		t.Error("mermaid output must start with 'graph TD'")
	}
	for _, want := range []string{"Home Network", "lab", "web", "80/tcp", "nginx"} {
		if !strings.Contains(out, want) {
			t.Errorf("mermaid output missing %q\n%s", want, out)
		}
	}
}

func TestRenderLinksDockerMappedPorts(t *testing.T) {
	inv := &Inventory{
		ServerName: "lab",
		Host:       "10.0.0.1",
		Containers: []docker.Container{
			{Name: "api", State: "running", Image: "api:latest", Ports: "0.0.0.0:8877->8877/tcp, [::]:8877->8877/tcp"},
		},
		Ports: []ports.PortInfo{
			{Port: "8877", Protocol: "tcp", Process: "ssh"},
		},
	}

	tree := RenderTree(inv)
	if !strings.Contains(tree, ":8877/tcp · api (forwarded by ssh)") {
		t.Fatalf("tree should show friendly forwarded container port\n%s", tree)
	}

	mermaid := RenderMermaid(inv)
	if !strings.Contains(mermaid, "c0 -. exposes .-> p0") {
		t.Fatalf("mermaid should link container to exposed host port\n%s", mermaid)
	}
}

func TestRenderDedupesIPv4IPv6DuplicatePorts(t *testing.T) {
	inv := &Inventory{
		ServerName: "lab",
		Host:       "10.0.0.1",
		Ports: []ports.PortInfo{
			{Address: "127.0.0.1", Port: "18789", Protocol: "tcp", Process: "node"},
			{Address: "[::1]", Port: "18789", Protocol: "tcp", Process: "node"},
			{Address: "*", Port: "80", Protocol: "tcp", Process: "nginx"},
		},
	}

	tree := RenderTree(inv)
	if got := strings.Count(tree, ":18789/tcp"); got != 1 {
		t.Fatalf("expected duplicate localhost IPv4/IPv6 port to render once, got %d\n%s", got, tree)
	}

	mermaid := RenderMermaid(inv)
	if got := strings.Count(mermaid, ":18789/tcp"); got != 1 {
		t.Fatalf("expected duplicate localhost IPv4/IPv6 port to render once in mermaid, got %d\n%s", got, mermaid)
	}
}

func TestRenderTreeFiltered_Exposed(t *testing.T) {
	inv := &Inventory{
		ServerName: "demo-lab",
		Host:       "10.0.0.1",
		Containers: []docker.Container{
			{Name: "app-api", State: "running", Image: "app:1", Ports: "0.0.0.0:8080->8080/tcp"},
		},
		Ports: []ports.PortInfo{
			{Port: "8080", Protocol: "tcp", Address: "0.0.0.0", Process: "docker-proxy"},
			{Port: "8443", Protocol: "tcp", Address: "0.0.0.0", Process: "dashboard"},
			{Port: "5432", Protocol: "tcp", Address: "127.0.0.1", Process: "postgres"},
		},
	}

	out, err := RenderTreeFiltered(inv, "exposed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"Home Network", "demo-lab", "Exposed Ports", "├─ :8080/tcp · app-api", "└─ :8443/tcp · dashboard"} {
		if !strings.Contains(out, want) {
			t.Errorf("exposed output missing %q\n%s", want, out)
		}
	}
	for _, notWant := range []string{"5432", "postgres", "App Ports", "Containers", "Summary"} {
		if strings.Contains(out, notWant) {
			t.Errorf("exposed output should not contain %q\n%s", notWant, out)
		}
	}
}

func TestRenderTreeFiltered_ExposedPublicIPv6(t *testing.T) {
	inv := &Inventory{
		ServerName: "demo-lab",
		Host:       "10.0.0.1",
		Ports: []ports.PortInfo{
			{Port: "443", Protocol: "tcp", Address: "[::]", Process: "nginx"},
			{Port: "22", Protocol: "tcp", Address: "127.0.0.1", Process: "sshd"},
		},
	}

	out, err := RenderTreeFiltered(inv, "exposed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, ":443/tcp · nginx") {
		t.Errorf("exposed output missing public IPv6 port\n%s", out)
	}
	if strings.Contains(out, ":22/tcp") {
		t.Errorf("exposed output should not contain loopback port\n%s", out)
	}
}

func TestRenderTreeFiltered_ExposedEmpty(t *testing.T) {
	inv := &Inventory{
		ServerName: "demo-lab",
		Host:       "10.0.0.1",
		Ports: []ports.PortInfo{
			{Port: "5432", Protocol: "tcp", Address: "127.0.0.1", Process: "postgres"},
		},
	}

	out, err := RenderTreeFiltered(inv, "exposed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected empty exposed view to show '(none)'\n%s", out)
	}
	if strings.Contains(out, "5432") {
		t.Errorf("empty exposed view should not contain local port\n%s", out)
	}
}

func TestRenderTreeFiltered_ExposedShowsWarnings(t *testing.T) {
	inv := &Inventory{
		ServerName: "demo-lab",
		Host:       "10.0.0.1",
		Ports:      []ports.PortInfo{},
		Warnings:   []string{"ports: failed to list ports: exit status 1"},
	}

	out, err := RenderTreeFiltered(inv, "exposed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Warnings (1)") || !strings.Contains(out, "failed to list ports") {
		t.Errorf("filtered view must surface collection warnings instead of a bare '(none)'\n%s", out)
	}
	if strings.Contains(out, "(none)") {
		t.Errorf("(none) must not print when the scan failed; it reads as an authoritative count\n%s", out)
	}
}

func TestRenderTreeFiltered_UnsupportedFilter(t *testing.T) {
	inv := &Inventory{ServerName: "lab", Host: "10.0.0.1"}

	_, err := RenderTreeFiltered(inv, "bogus")
	if err == nil {
		t.Fatal("expected error for unsupported filter")
	}
	if !strings.Contains(err.Error(), "unsupported filter") || !strings.Contains(err.Error(), "exposed") {
		t.Errorf("unhelpful error for unsupported filter: %v", err)
	}
}

func TestSupportedFilters(t *testing.T) {
	got := SupportedFilters()
	if len(got) != 1 || got[0] != "exposed" {
		t.Errorf("SupportedFilters() = %v, want [exposed]", got)
	}
}

func TestIsSupportedFilter(t *testing.T) {
	if !IsSupportedFilter("exposed") {
		t.Error("expected 'exposed' to be a supported filter")
	}
	for _, f := range []string{"", "bogus", "public"} {
		if IsSupportedFilter(f) {
			t.Errorf("did not expect %q to be a supported filter", f)
		}
	}
}

func TestJSON_Roundtrip(t *testing.T) {
	inv := &Inventory{
		ServerName: "s1",
		Host:       "h1",
		System:     &system.StatusInfo{Hostname: "h1"},
		Containers: []docker.Container{{Name: "c1"}},
		Ports:      []ports.PortInfo{{Port: "22", Protocol: "tcp"}},
		Warnings:   []string{"w1"},
	}

	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Inventory
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ServerName != "s1" || got.Warnings[0] != "w1" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
