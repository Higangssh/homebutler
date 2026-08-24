package mcp

import (
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
	if _, err := s.executeTool("proxmox_status", nil); err == nil || !strings.Contains(err.Error(), "multiple Proxmox endpoints") {
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

func readProxmoxFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "proxmox", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
