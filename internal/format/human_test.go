package format

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

func TestStatus(t *testing.T) {
	in := &system.StatusInfo{
		Hostname: "homelab-server",
		OS:       "linux",
		Arch:     "amd64",
		Uptime:   "1d 2h",
		CPU:      system.CPUInfo{UsagePercent: 12.3, Cores: 8},
		Memory:   system.MemInfo{UsedGB: 4.5, TotalGB: 16, Percent: 28.1},
		Disks:    []system.DiskInfo{{Mount: "/", UsedGB: 30, TotalGB: 100, Percent: 30}},
	}
	out := Status(in)
	for _, want := range []string{"homelab-server", "linux/amd64", "CPU:", "Memory:", "Disk /:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %s", want, out)
		}
	}
}

func TestDockerListAndAction(t *testing.T) {
	if got := DockerList(nil); got != "No containers found.\n" {
		t.Fatalf("unexpected empty message: %q", got)
	}
	out := DockerList([]docker.Container{{Name: "nginx", Image: "nginx:latest", State: "running", Status: "Up 1h"}})
	if !strings.Contains(out, "nginx") || !strings.Contains(out, "IMAGE") {
		t.Fatalf("unexpected docker list output: %s", out)
	}
	if got := DockerAction("restart", "nginx"); !strings.Contains(got, "restart") || !strings.Contains(got, "nginx") {
		t.Fatalf("unexpected action output: %s", got)
	}
}

func TestAlerts(t *testing.T) {
	res := &alerts.AlertResult{
		CPU:    alerts.AlertItem{Current: 10, Threshold: 90, Status: "ok"},
		Memory: alerts.AlertItem{Current: 75, Threshold: 85, Status: "warning"},
		Disks:  []alerts.DiskAlert{{Mount: "/", Current: 95, Threshold: 90, Status: "critical"}},
	}
	out := Alerts(res)
	for _, want := range []string{"✅", "⚠️", "🔴", "Disk /"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %s", want, out)
		}
	}
}

func TestPortsNetworkWake(t *testing.T) {
	if got := Ports(nil); got != "No open ports found.\n" {
		t.Fatalf("unexpected empty ports: %q", got)
	}
	portsOut := Ports([]ports.PortInfo{{Protocol: "tcp", Address: "0.0.0.0", Port: "80", PID: "123", Process: "nginx"}})
	if !strings.Contains(portsOut, "nginx/123") {
		t.Fatalf("unexpected ports output: %s", portsOut)
	}

	if got := NetworkScan(nil); got != "No devices found.\n" {
		t.Fatalf("unexpected empty network scan: %q", got)
	}
	netOut := NetworkScan([]network.Device{{IP: "192.168.1.10", MAC: "aa:bb", Hostname: "pi"}, {IP: "192.168.1.11", MAC: "cc:dd"}})
	if !strings.Contains(netOut, "2 devices found") || !strings.Contains(netOut, "-") {
		t.Fatalf("unexpected network output: %s", netOut)
	}

	if got := WakeResult("aa:bb", "255.255.255.255"); !strings.Contains(got, "Magic packet") {
		t.Fatalf("unexpected wake output: %s", got)
	}
}

func TestMultiServerAndHelpers(t *testing.T) {
	out := MultiServer([]map[string]interface{}{
		{"server": "s1", "error": "offline"},
		{"server": "s2", "data": map[string]interface{}{"cpu": map[string]interface{}{"usage_percent": 11.0}, "memory": map[string]interface{}{"usage_percent": 22.0}, "disks": []interface{}{map[string]interface{}{"usage_percent": 33.0}}, "uptime": "1d"}},
		{"server": "s3", "data": "bad"},
	})
	for _, want := range []string{"s1", "offline", "s2", "CPU", "s3", "no data"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %s", want, out)
		}
	}

	if got := statusIcon("unknown"); got != "unknown" {
		t.Fatalf("statusIcon fallback failed: %s", got)
	}
	if got := getNestedFloat(map[string]interface{}{"a": map[string]interface{}{"b": 1.5}}, "a", "b"); got != 1.5 {
		t.Fatalf("unexpected nested float: %v", got)
	}
	if got := getFirstDiskPercent(map[string]interface{}{"disks": []interface{}{map[string]interface{}{"usage_percent": 44.0}}}); got != 44.0 {
		t.Fatalf("unexpected disk percent: %v", got)
	}
}
