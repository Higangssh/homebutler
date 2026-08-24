package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

func TestSelectProxmoxEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []config.ProxmoxConfig
		selected  string
		want      string
		err       string
	}{
		{name: "implicit single", endpoints: []config.ProxmoxConfig{{Name: "pve"}}, want: "pve"},
		{name: "explicit", endpoints: []config.ProxmoxConfig{{Name: "pve1"}, {Name: "pve2"}}, selected: "pve2", want: "pve2"},
		{name: "none configured", err: "no Proxmox endpoints configured"},
		{name: "multiple require endpoint", endpoints: []config.ProxmoxConfig{{Name: "pve1"}, {Name: "pve2"}}, err: "multiple proxmox endpoints configured; use --endpoint"},
		{name: "unknown endpoint", endpoints: []config.ProxmoxConfig{{Name: "pve"}}, selected: "other", err: "proxmox endpoint \"other\" not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := selectProxmoxEndpoint(&config.Config{Proxmox: tt.endpoints}, tt.selected)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("selectProxmoxEndpoint() error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || endpoint.Name != tt.want {
				t.Fatalf("selectProxmoxEndpoint() = %#v, %v; want %q", endpoint, err, tt.want)
			}
		})
	}
}

func TestProxmoxStatusJSON(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/status":    "cluster-status-cluster.json",
		"/api2/json/cluster/resources": "cluster-resources.json",
	}
	called := make(map[string]bool)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "PVEAPIToken=monitoring@pve!readonly=test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		fixture, ok := fixtures[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		called[r.URL.Path] = true
		_, _ = w.Write(readProxmoxFixture(t, fixture))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "homebutler.yaml")
	configData := fmt.Sprintf("proxmox:\n  - name: pve\n    host: %s\n    port: %d\n    token_id: monitoring@pve!readonly\n    token: test-token\n    insecure: true\n", host, port)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	oldPath, oldJSON, oldCfg := cfgPath, jsonOutput, cfg
	defer func() { cfgPath, jsonOutput, cfg = oldPath, oldJSON, oldCfg }()
	cfgPath, jsonOutput, cfg = configPath, true, nil
	cmd := newProxmoxStatusCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("proxmox status: %v", err)
	}

	var view struct {
		Version struct {
			Version string `json:"version"`
		} `json:"version"`
		Cluster   []json.RawMessage `json:"cluster"`
		Resources struct {
			Guests []json.RawMessage `json:"guests"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(output.Bytes(), &view); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, output.String())
	}
	if view.Version.Version != "9.1.4" || len(view.Cluster) != 3 || len(view.Resources.Guests) != 3 {
		t.Errorf("unexpected status view: %#v", view)
	}
	for path := range fixtures {
		if !called[path] {
			t.Errorf("did not request %s", path)
		}
	}
}

func TestProxmoxStatusRejectsServerFlags(t *testing.T) {
	oldServer, oldAll := serverName, allServers
	defer func() { serverName, allServers = oldServer, oldAll }()
	serverName = "ssh-host"
	if err := newProxmoxStatusCmd().Execute(); err == nil || !strings.Contains(err.Error(), "do not support --server") {
		t.Fatalf("proxmox status with --server error = %v", err)
	}
}

func TestProxmoxStatusHumanSummary(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/status":    "cluster-status-cluster.json",
		"/api2/json/cluster/resources": "cluster-resources.json",
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readProxmoxFixture(t, fixtures[r.URL.Path]))
	}))
	defer server.Close()

	configPath := writeProxmoxTestConfig(t, server.URL)
	oldPath, oldJSON, oldCfg := cfgPath, jsonOutput, cfg
	defer func() { cfgPath, jsonOutput, cfg = oldPath, oldJSON, oldCfg }()
	cfgPath, jsonOutput, cfg = configPath, false, nil
	cmd := newProxmoxStatusCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("proxmox status: %v", err)
	}
	for _, want := range []string{"Cluster: lab-cluster | quorum: yes | nodes: 1/2 online", "Resources: 2 nodes | 3 guests | 1 storage"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("status output missing %q:\n%s", want, output.String())
		}
	}
}

func TestProxmoxGuestsFiltersFixture(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("path = %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(readProxmoxFixture(t, "cluster-resources.json"))
	}))
	defer server.Close()

	configPath := writeProxmoxTestConfig(t, server.URL)
	oldPath, oldJSON, oldCfg := cfgPath, jsonOutput, cfg
	defer func() { cfgPath, jsonOutput, cfg = oldPath, oldJSON, oldCfg }()
	cfgPath, jsonOutput, cfg = configPath, true, nil
	cmd := newProxmoxGuestsCmd()
	cmd.SetArgs([]string{"--node", "pve1", "--status", "running"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("proxmox guests: %v", err)
	}

	var guests []struct {
		VMID   int    `json:"vmid"`
		Node   string `json:"node"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &guests); err != nil {
		t.Fatalf("decode guests JSON: %v", err)
	}
	if len(guests) != 2 || guests[0].VMID != 100 || guests[1].VMID != 104 || guests[0].Node != "pve1" || guests[0].Status != "running" {
		t.Errorf("guests = %#v", guests)
	}
}

func TestProxmoxNodeAndTasksFixtures(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/pve1/status":
			_, _ = w.Write(readProxmoxFixture(t, "node-status-pve9.json"))
		case "/api2/json/nodes/pve1/tasks":
			if got := r.URL.Query().Get("source"); got != "all" {
				t.Errorf("tasks source = %q, want all", got)
			}
			if got := r.URL.Query().Get("limit"); got != "7" {
				t.Errorf("tasks limit = %q, want 7", got)
			}
			_, _ = w.Write(readProxmoxFixture(t, "tasks-node.json"))
		default:
			t.Errorf("path = %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeProxmoxTestConfig(t, server.URL)
	oldPath, oldJSON, oldCfg := cfgPath, jsonOutput, cfg
	defer func() { cfgPath, jsonOutput, cfg = oldPath, oldJSON, oldCfg }()
	cfgPath, jsonOutput, cfg = configPath, true, nil

	nodeCmd := newProxmoxNodeCmd()
	nodeCmd.SetArgs([]string{"pve1"})
	var nodeOutput bytes.Buffer
	nodeCmd.SetOut(&nodeOutput)
	if err := nodeCmd.Execute(); err != nil {
		t.Fatalf("proxmox node: %v", err)
	}
	var node struct {
		PVEVersion string `json:"pveversion"`
	}
	if err := json.Unmarshal(nodeOutput.Bytes(), &node); err != nil || node.PVEVersion == "" {
		t.Fatalf("node JSON = %#v, %v", node, err)
	}

	tasksCmd := newProxmoxTasksCmd()
	tasksCmd.SetArgs([]string{"--node", "pve1", "--limit", "7"})
	var tasksOutput bytes.Buffer
	tasksCmd.SetOut(&tasksOutput)
	if err := tasksCmd.Execute(); err != nil {
		t.Fatalf("proxmox tasks: %v", err)
	}
	var tasks []struct {
		UPID string `json:"upid"`
	}
	if err := json.Unmarshal(tasksOutput.Bytes(), &tasks); err != nil || len(tasks) != 3 {
		t.Fatalf("tasks JSON = %#v, %v", tasks, err)
	}
}

func TestProxmoxTasksAggregateNodes(t *testing.T) {
	called := make(map[string]bool)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called[r.URL.Path] = true
		switch r.URL.Path {
		case "/api2/json/cluster/resources":
			_, _ = w.Write(readProxmoxFixture(t, "cluster-resources.json"))
		case "/api2/json/nodes/pve1/tasks", "/api2/json/nodes/pve2/tasks":
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Errorf("tasks limit = %q, want 50", got)
			}
			_, _ = w.Write(readProxmoxFixture(t, "tasks-node.json"))
		default:
			t.Errorf("unexpected task request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeProxmoxTestConfig(t, server.URL)
	oldPath, oldJSON, oldCfg := cfgPath, jsonOutput, cfg
	defer func() { cfgPath, jsonOutput, cfg = oldPath, oldJSON, oldCfg }()
	cfgPath, jsonOutput, cfg = configPath, true, nil
	cmd := newProxmoxTasksCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("proxmox tasks: %v", err)
	}
	var tasks []json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &tasks); err != nil || len(tasks) != 6 {
		t.Fatalf("tasks JSON count = %d, error = %v", len(tasks), err)
	}
	for _, path := range []string{"/api2/json/cluster/resources", "/api2/json/nodes/pve1/tasks", "/api2/json/nodes/pve2/tasks"} {
		if !called[path] {
			t.Errorf("did not request %s", path)
		}
	}
}

func TestProxmoxTasksRequirePositiveLimit(t *testing.T) {
	cmd := newProxmoxTasksCmd()
	cmd.SetArgs([]string{"--node", "pve1", "--limit", "0"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--limit must be positive") {
		t.Fatalf("tasks limit error = %v", err)
	}
}

func TestProxmoxGuestsRejectsInvalidStatus(t *testing.T) {
	cmd := newProxmoxGuestsCmd()
	cmd.SetArgs([]string{"--status", "paused"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--status must be running or stopped") {
		t.Fatalf("guests status error = %v", err)
	}
}

func writeProxmoxTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "homebutler.yaml")
	data := fmt.Sprintf("proxmox:\n  - name: pve\n    host: %s\n    port: %d\n    token_id: monitoring@pve!readonly\n    token: test-token\n    insecure: true\n", host, port)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readProxmoxFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "proxmox", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
