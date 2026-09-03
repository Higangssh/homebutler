package mcp

import (
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/proxmox"
)

func TestProxmoxToolsUseConfiguredEndpoint(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/status":    "cluster-status-cluster.json",
		"/api2/json/cluster/resources": "cluster-resources.json",
		"/api2/json/nodes/pve1/status": "node-status-pve9.json",
		"/api2/json/nodes/pve1/tasks":  "tasks-node.json",
	}
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "PVEAPIToken=monitoring@pve!readonly=fixture-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path == "/api2/json/nodes/pve1/tasks" && r.URL.RawQuery != "limit=50&source=all" {
			t.Errorf("tasks query = %q", r.URL.RawQuery)
		}
		requests = append(requests, r.URL.RequestURI())
		name, ok := fixtures[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(readProxmoxFixture(t, name))
	}))
	defer server.Close()

	s := testProxmoxMCPServer(t, server.URL)

	status, err := s.executeTool("proxmox_status", nil)
	if err != nil {
		t.Fatalf("proxmox_status: %v", err)
	}
	view, ok := status.(proxmox.DefaultView)
	if !ok || view.Version.Version != "9.1.4" || len(view.Resources.Guests) != 3 {
		t.Fatalf("proxmox_status = %#v", status)
	}

	guests, err := s.executeTool("proxmox_guests", map[string]any{"status": "stopped", "type": "qemu", "node": "pve1"})
	if err != nil {
		t.Fatalf("proxmox_guests: %v", err)
	}
	filtered := guests.([]proxmox.Guest)
	if len(filtered) != 1 || filtered[0].VMID != 101 {
		t.Fatalf("proxmox_guests filtered = %#v", filtered)
	}

	node, err := s.executeTool("proxmox_node", map[string]any{"node": "pve1"})
	if err != nil {
		t.Fatalf("proxmox_node: %v", err)
	}
	if got := node.(proxmox.NodeStatus).CPUInfo.CPUs; got != 16 {
		t.Errorf("proxmox_node CPUs = %d, want 16", got)
	}

	tasks, err := s.executeTool("proxmox_tasks", map[string]any{"node": "pve1"})
	if err != nil {
		t.Fatalf("proxmox_tasks: %v", err)
	}
	if got := len(tasks.([]proxmox.Task)); got != 3 {
		t.Errorf("proxmox_tasks count = %d, want 3", got)
	}

	if got := strings.Join(requests, ","); !strings.Contains(got, "/api2/json/version") || !strings.Contains(got, "/api2/json/nodes/pve1/tasks?limit=50&source=all") {
		t.Errorf("requests = %q", got)
	}
}

func TestProxmoxToolSelectionAndArguments(t *testing.T) {
	s := NewServer(&config.Config{Proxmox: []config.ProxmoxConfig{{Name: "pve1"}, {Name: "pve2"}}}, "test")
	if _, err := s.executeTool("proxmox_status", nil); err == nil || !strings.Contains(err.Error(), "multiple Proxmox endpoints configured; specify endpoint") {
		t.Errorf("multiple endpoints error = %v", err)
	}
	if _, err := s.executeTool("proxmox_status", map[string]any{"endpoint": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing endpoint error = %v", err)
	}
	if _, err := s.executeTool("proxmox_node", map[string]any{}); err == nil || !strings.Contains(err.Error(), "missing required parameter: node") {
		t.Errorf("missing node error = %v", err)
	}
	if _, err := s.executeTool("proxmox_status", map[string]any{"server": "pve1"}); err == nil || !strings.Contains(err.Error(), "cannot be pointed at a server") {
		t.Errorf("server routing error = %v", err)
	}
}

func TestProxmoxToolDefinitionsAreReadOnlyAPIEndpoints(t *testing.T) {
	for _, name := range []string{"proxmox_status", "proxmox_guests", "proxmox_node", "proxmox_tasks"} {
		cap, ok := capabilityFor(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if cap.risk != riskRead || !cap.supports(targetProxmox) || cap.supports(targetLocal) || cap.supports(targetServer) {
			t.Errorf("%s capability = %#v", name, cap)
		}
	}
}

func TestProxmoxPhase2Dispatch(t *testing.T) {
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.EscapedPath())
		isTaskStatus := strings.HasSuffix(r.URL.Path, "/status") && strings.Contains(r.URL.Path, "/tasks/")
		want := "PVEAPIToken=power@pve!actions=fixture-action-token"
		if isTaskStatus {
			want = "PVEAPIToken=monitoring@pve!readonly=fixture-token"
		}
		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if isTaskStatus {
			if r.Method != http.MethodGet {
				t.Errorf("task method = %q, want GET", r.Method)
			}
			_, _ = w.Write(readProxmoxFixture(t, "task-status-stopped-ok.json"))
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("action method = %q, want POST", r.Method)
		}
		_, _ = w.Write([]byte(`{"data":"UPID:pve1:opaque"}`))
	}))
	defer server.Close()
	s := testProxmoxMCPServerWithAction(t, server.URL)

	tests := []struct {
		name       string
		action     proxmox.GuestAction
		guestType  string
		wantSuffix string
	}{
		{name: "proxmox_guest_start", action: proxmox.GuestActionStart, guestType: "qemu", wantSuffix: "/qemu/100/status/start"},
		{name: "proxmox_guest_reboot", action: proxmox.GuestActionReboot, guestType: "lxc", wantSuffix: "/lxc/100/status/reboot"},
		{name: "proxmox_guest_shutdown", action: proxmox.GuestActionShutdown, guestType: "qemu", wantSuffix: "/qemu/100/status/shutdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.executeTool(tt.name, map[string]any{
				"endpoint": "pve", "node": "pve1", "type": tt.guestType, "vmid": float64(100), "confirm": true,
			})
			if err != nil {
				t.Fatal(err)
			}
			action := result.(proxmoxGuestActionResult)
			if action.Status != "accepted" || action.Action != tt.action || action.UPID != "UPID:pve1:opaque" || action.Type != tt.guestType {
				t.Errorf("result = %#v", action)
			}
			if got := requests[len(requests)-1]; !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("path = %q, want suffix %q", got, tt.wantSuffix)
			}
		})
	}

	status, err := s.executeTool("proxmox_task_status", map[string]any{"endpoint": "pve", "node": "pve1", "upid": "UPID:opaque"})
	if err != nil {
		t.Fatal(err)
	}
	task := status.(proxmox.TaskStatus)
	if task.Status != "stopped" || task.ExitStatus != "OK" || task.Result != "ok" {
		t.Errorf("task status = %#v", task)
	}
}

func TestProxmoxGuestActionRequiresActionCredential(t *testing.T) {
	var requests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	s := testProxmoxMCPServer(t, server.URL)

	_, err := s.executeTool("proxmox_guest_start", map[string]any{
		"endpoint": "pve", "node": "pve1", "type": "qemu", "vmid": float64(100), "confirm": true,
	})
	if err == nil || !strings.Contains(err.Error(), "no action credential configured for Proxmox endpoint \"pve\"") {
		t.Fatalf("error = %v, want no action credential message", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 (no network call before the credential check)", requests)
	}
}

func TestProxmoxPhase2CapabilityMetadataAndSchemas(t *testing.T) {
	tests := []struct {
		name     string
		risk     capabilityRisk
		required []string
	}{
		{name: "proxmox_guest_start", risk: riskWrite, required: []string{"endpoint", "node", "type", "vmid", "confirm"}},
		{name: "proxmox_guest_reboot", risk: riskWrite, required: []string{"endpoint", "node", "type", "vmid", "confirm"}},
		{name: "proxmox_guest_shutdown", risk: riskDestructive, required: []string{"endpoint", "node", "type", "vmid", "confirm"}},
		{name: "proxmox_task_status", risk: riskRead, required: []string{"endpoint", "node", "upid"}},
	}
	for _, tt := range tests {
		cap, ok := capabilityFor(tt.name)
		if !ok {
			t.Fatalf("%s is not registered", tt.name)
		}
		if cap.risk != tt.risk || !cap.supports(targetProxmox) || cap.supports(targetLocal) || cap.supports(targetServer) {
			t.Errorf("%s capability = %#v", tt.name, cap)
		}
		if got := strings.Join(cap.tool.InputSchema.Required, ","); got != strings.Join(tt.required, ",") {
			t.Errorf("%s required = %q", tt.name, got)
		}
	}
	start, _ := capabilityFor("proxmox_guest_start")
	if start.tool.InputSchema.Properties["vmid"].Type != "integer" || start.tool.InputSchema.Properties["confirm"].Type != "boolean" {
		t.Errorf("action schema = %#v", start.tool.InputSchema)
	}
}

func TestProxmoxPhase2ValidationPrecedesCredentials(t *testing.T) {
	s := NewServer(&config.Config{Proxmox: []config.ProxmoxConfig{{
		Name: "pve", Host: "127.0.0.1", TokenID: "fixture@pve!token", TokenFile: filepath.Join(t.TempDir(), "missing-token"),
	}}}, "test")
	valid := map[string]any{"endpoint": "pve", "node": "pve1", "type": "qemu", "vmid": float64(100), "confirm": true}
	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{name: "missing endpoint", change: func(a map[string]any) { delete(a, "endpoint") }},
		{name: "missing node", change: func(a map[string]any) { delete(a, "node") }},
		{name: "invalid type", change: func(a map[string]any) { a["type"] = "vm" }},
		{name: "string VMID", change: func(a map[string]any) { a["vmid"] = "100" }},
		{name: "fractional VMID", change: func(a map[string]any) { a["vmid"] = 100.5 }},
		{name: "NaN VMID", change: func(a map[string]any) { a["vmid"] = math.NaN() }},
		{name: "out of range VMID", change: func(a map[string]any) { a["vmid"] = float64(0) }},
		{name: "missing confirmation", change: func(a map[string]any) { delete(a, "confirm") }},
		{name: "false confirmation", change: func(a map[string]any) { a["confirm"] = false }},
		{name: "string confirmation", change: func(a map[string]any) { a["confirm"] = "true" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := make(map[string]any, len(valid))
			for key, value := range valid {
				args[key] = value
			}
			tt.change(args)
			if _, err := s.executeTool("proxmox_guest_start", args); err == nil || strings.Contains(err.Error(), "read token") {
				t.Errorf("error = %v", err)
			}
		})
	}

	for _, args := range []map[string]any{
		{"node": "pve1", "upid": "UPID:opaque"},
		{"endpoint": "pve", "upid": "UPID:opaque"},
		{"endpoint": "pve", "node": "pve1"},
	} {
		if _, err := s.executeTool("proxmox_task_status", args); err == nil || strings.Contains(err.Error(), "read token") {
			t.Errorf("task validation error = %v", err)
		}
	}
}

func TestProxmoxPhase2ConfirmationErrorIdentifiesTarget(t *testing.T) {
	s := NewServer(&config.Config{}, "test")
	_, err := s.executeTool("proxmox_guest_shutdown", map[string]any{
		"endpoint": "pve", "node": "pve1", "type": "lxc", "vmid": float64(101), "confirm": false,
	})
	for _, want := range []string{`endpoint="pve"`, `node="pve1"`, `type="lxc"`, "vmid=101", `action="shutdown"`, "confirm to true"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("confirmation error = %v, want %q", err, want)
		}
	}
}

func TestProxmoxPhase2Demo(t *testing.T) {
	s := NewServer(&config.Config{}, "test", true)
	args := map[string]any{"endpoint": "pve", "node": "pve1", "type": "qemu", "vmid": 100, "confirm": true}
	for _, name := range []string{"proxmox_guest_start", "proxmox_guest_reboot", "proxmox_guest_shutdown"} {
		result, err := s.executeDemoTool(name, args)
		if err != nil || result.(proxmoxGuestActionResult).Status != "accepted" {
			t.Errorf("%s demo = %#v, %v", name, result, err)
		}
	}
	result, err := s.executeDemoTool("proxmox_task_status", map[string]any{"endpoint": "pve", "node": "pve1", "upid": "UPID:opaque"})
	if err != nil || result.(proxmox.TaskStatus).Result != "ok" {
		t.Errorf("task demo = %#v, %v", result, err)
	}
}

func testProxmoxMCPServer(t *testing.T, rawURL string) *Server {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(&config.Config{Proxmox: []config.ProxmoxConfig{{
		Name: "pve", Host: u.Hostname(), Port: port, TokenID: "monitoring@pve!readonly", Token: "fixture-token", Insecure: true,
	}}}, "test")
}

func testProxmoxMCPServerWithAction(t *testing.T, rawURL string) *Server {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(&config.Config{Proxmox: []config.ProxmoxConfig{{
		Name: "pve", Host: u.Hostname(), Port: port, Insecure: true,
		TokenID: "monitoring@pve!readonly", Token: "fixture-token",
		ActionTokenID: "power@pve!actions", ActionToken: "fixture-action-token",
	}}}, "test")
}

func readProxmoxFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "proxmox", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
