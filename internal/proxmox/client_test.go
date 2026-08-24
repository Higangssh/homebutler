package proxmox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientRequests(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/status":    "cluster-status-standalone.json",
		"/api2/json/cluster/resources": "cluster-resources.json",
		"/api2/json/nodes/pve1/status": "node-status-pve9.json",
		"/api2/json/nodes/pve1/tasks":  "tasks-node.json",
	}
	called := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "PVEAPIToken=monitoring@pve!readonly=fixture-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if r.URL.Path == "/api2/json/nodes/pve1/tasks" {
			if got, want := r.URL.Query().Get("source"), "all"; got != want {
				t.Errorf("tasks source = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("limit"), "50"; got != want {
				t.Errorf("tasks limit = %q, want %q", got, want)
			}
		}
		name, ok := fixtures[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		called[r.URL.Path] = true
		_, _ = w.Write(readFixture(t, name))
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	ctx := context.Background()
	if version, err := client.Version(ctx); err != nil || version.Version != "9.1.4" {
		t.Fatalf("Version() = %#v, %v", version, err)
	}
	if status, err := client.ClusterStatus(ctx); err != nil || len(status) != 1 || !status[0].Local || !status[0].Online {
		t.Fatalf("ClusterStatus() = %#v, %v", status, err)
	}
	if resources, err := client.Resources(ctx); err != nil || len(resources.Guests) != 3 {
		t.Fatalf("Resources() = %#v, %v", resources, err)
	}
	if status, err := client.NodeStatus(ctx, "pve1"); err != nil || status.CPUInfo.CPUs != 16 {
		t.Fatalf("NodeStatus() = %#v, %v", status, err)
	}
	if tasks, err := client.Tasks(ctx, "pve1"); err != nil || len(tasks) != 3 {
		t.Fatalf("Tasks() = %#v, %v", tasks, err)
	}
	for path := range fixtures {
		if !called[path] {
			t.Errorf("did not request %s", path)
		}
	}
}

func TestDefaultView(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/status":    "cluster-status-cluster.json",
		"/api2/json/cluster/resources": "cluster-resources.json",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, fixtures[r.URL.Path]))
	}))
	defer server.Close()

	view, err := testClient(t, server.URL).DefaultView(context.Background())
	if err != nil {
		t.Fatalf("DefaultView() error: %v", err)
	}
	if view.Version.Release != "9.1" || len(view.Cluster) != 3 || len(view.Resources.Storage) != 1 {
		t.Errorf("DefaultView() = %#v", view)
	}
}

func TestDefaultViewKeepsSuccessfulResponses(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/resources": "cluster-resources.json",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/cluster/status" {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		_, _ = w.Write(readFixture(t, fixtures[r.URL.Path]))
	}))
	defer server.Close()

	view, err := testClient(t, server.URL).DefaultView(context.Background())
	if err != nil {
		t.Fatalf("DefaultView() error: %v", err)
	}
	if view.Version.Version != "9.1.4" || len(view.Resources.Guests) != 3 || !view.CollectorFailed("cluster") || len(view.Warnings) != 1 {
		t.Errorf("DefaultView() = %#v", view)
	}
}

func TestResourcesNormalizesFixture(t *testing.T) {
	server := fixtureServer(t, "cluster-resources.json")
	defer server.Close()

	resources, err := testClient(t, server.URL).Resources(context.Background())
	if err != nil {
		t.Fatalf("Resources() error: %v", err)
	}
	if len(resources.Nodes) != 2 || len(resources.Storage) != 1 || len(resources.Guests) != 3 {
		t.Fatalf("resources = %#v", resources)
	}
	if resources.Nodes[0].Name != "pve1" || resources.Nodes[1].Name != "pve2" {
		t.Errorf("resource node names = %#v", resources.Nodes)
	}
	if resources.Nodes[1].CPU != nil || resources.Nodes[1].Mem != nil {
		t.Errorf("missing node metrics = %#v, want nil", resources.Nodes[1])
	}
	qemu := resources.Guests[0]
	if qemu.Template || qemu.Disk == nil || *qemu.Disk != 0 || qemu.NetOut == nil || *qemu.NetOut != 1036055258639 {
		t.Errorf("QEMU normalization = %#v", qemu)
	}
	stopped := resources.Guests[1]
	if stopped.CPU == nil || *stopped.CPU != 0 || stopped.Mem == nil || *stopped.Mem != 0 {
		t.Errorf("stopped guest metrics = %#v; explicit zero must be retained", stopped)
	}
	lxc := resources.Guests[2]
	if !lxc.Template || lxc.Lock != "backup" || strings.Join(lxc.Tags, ",") != "community-script,monitoring,network,visualization" || lxc.CPU == nil || *lxc.CPU != 3.44330117252769e-05 {
		t.Errorf("LXC normalization = %#v", lxc)
	}
	store := resources.Storage[0]
	if store.Used == nil || *store.Used != 435223727072 || store.Total == nil || *store.Total != 852878163968 || store.Shared {
		t.Errorf("storage normalization = %#v", store)
	}
}

func TestNodeStatusAndTasksFixtures(t *testing.T) {
	t.Run("node status", func(t *testing.T) {
		server := fixtureServer(t, "node-status-pve9.json")
		defer server.Close()
		status, err := testClient(t, server.URL).NodeStatus(context.Background(), "pve1")
		if err != nil {
			t.Fatal(err)
		}
		if status.BootInfo.SecureBoot || status.CPU == nil || *status.CPU != 0.012113083387472 || status.Memory.Total == nil || *status.Memory.Total != 30261309440 {
			t.Errorf("NodeStatus() = %#v", status)
		}
	})
	t.Run("tasks", func(t *testing.T) {
		server := fixtureServer(t, "tasks-node.json")
		defer server.Close()
		tasks, err := testClient(t, server.URL).Tasks(context.Background(), "pve1")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 3 || !strings.Contains(tasks[0].Status, "failed: exit code 1") || tasks[1].EndTime == nil || tasks[2].EndTime != nil {
			t.Errorf("Tasks() = %#v", tasks)
		}
	})
}

func TestResourcesEmptyForInsufficientACLs(t *testing.T) {
	server := fixtureServer(t, "cluster-resources-empty-acl.json")
	defer server.Close()
	resources, err := testClient(t, server.URL).Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Nodes) != 0 || len(resources.Guests) != 0 || len(resources.Storage) != 0 {
		t.Errorf("Resources() = %#v, want empty resources", resources)
	}
}

func TestAPIErrorIncludesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(readFixture(t, "error-403-permission.json"))
	}))
	defer server.Close()

	_, err := testClient(t, server.URL).Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden") || !strings.Contains(err.Error(), "Permission check failed (/, Sys.Audit)") {
		t.Errorf("Version() error = %v", err)
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write(readFixture(t, "version-pve9.json"))
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	client.http.Timeout = 5 * time.Millisecond
	_, err := client.Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Client.Timeout") {
		t.Errorf("Version() timeout error = %v", err)
	}
}

func TestTLSModes(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readFixture(t, "version-pve9.json"))
	}))
	defer server.Close()

	t.Run("fingerprint success", func(t *testing.T) {
		sum := sha256.Sum256(server.Certificate().Raw)
		client := newTLSClient(t, server.URL, Options{Fingerprint: colonHex(sum[:])})
		if _, err := client.Version(context.Background()); err != nil {
			t.Fatalf("Version() error: %v", err)
		}
	})
	t.Run("fingerprint mismatch", func(t *testing.T) {
		client := newTLSClient(t, server.URL, Options{Fingerprint: strings.Repeat("00:", 31) + "00"})
		if _, err := client.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
			t.Errorf("Version() error = %v", err)
		}
	})
	t.Run("CA file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pve-ca.pem")
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		client := newTLSClient(t, server.URL, Options{CAFile: path})
		if _, err := client.Version(context.Background()); err != nil {
			t.Fatalf("Version() error: %v", err)
		}
	})
	t.Run("explicit insecure", func(t *testing.T) {
		client := newTLSClient(t, server.URL, Options{Insecure: true})
		if _, err := client.Version(context.Background()); err != nil {
			t.Fatalf("Version() error: %v", err)
		}
	})
}

func TestNewValidation(t *testing.T) {
	for _, options := range []Options{
		{TokenID: "id", Token: "token"},
		{Host: "pve.example", Token: "token"},
		{Host: "pve.example", TokenID: "id"},
		{Host: "pve.example", TokenID: "id", Token: "token", Port: 70000},
		{Host: "pve.example", TokenID: "id", Token: "token", Timeout: -time.Second},
		{Host: "pve.example", TokenID: "id", Token: "token", Fingerprint: "bad"},
	} {
		if _, err := New(options); err == nil {
			t.Errorf("New(%+v) error = nil", options)
		}
	}
}

func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Options{Host: "pve.example", TokenID: "monitoring@pve!readonly", Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = baseURL
	return client
}

func newTLSClient(t *testing.T, baseURL string, options Options) *Client {
	t.Helper()
	options.Host = "127.0.0.1"
	options.TokenID = "monitoring@pve!readonly"
	options.Token = "fixture-token"
	client, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = baseURL
	return client
}

func fixtureServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readFixture(t, name))
	}))
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func colonHex(value []byte) string {
	encoded := strings.ToUpper(hex.EncodeToString(value))
	parts := make([]string, 0, len(encoded)/2)
	for i := 0; i < len(encoded); i += 2 {
		parts = append(parts, encoded[i:i+2])
	}
	return strings.Join(parts, ":")
}
