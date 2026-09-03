package proxmox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
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
	if view.Version.Version != "9.1.4" || len(view.Resources.Guests) != 3 || !view.CollectorFailed(CollectorCluster) || len(view.Warnings) != 1 {
		t.Errorf("DefaultView() = %#v", view)
	}
	data, err := json.Marshal(view)
	if err != nil || !strings.Contains(string(data), `"failed_collectors"`) || strings.Contains(string(data), `"failed":`) {
		t.Fatalf("DefaultView JSON = %s, error = %v", data, err)
	}
}

func TestDefaultViewFlagsNoVisibleResources(t *testing.T) {
	fixtures := map[string]string{
		"/api2/json/version":           "version-pve9.json",
		"/api2/json/cluster/status":    "cluster-status-cluster.json",
		"/api2/json/cluster/resources": "cluster-resources-empty-acl.json",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, fixtures[r.URL.Path]))
	}))
	defer server.Close()

	view, err := testClient(t, server.URL).DefaultView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !view.CollectorFailed(CollectorResources) || len(view.Warnings) != 1 || !strings.Contains(view.Warnings[0], "token permissions") {
		t.Fatalf("DefaultView() = %#v", view)
	}
}

// countingRoundTripper always fails, so it stands in for a host that never
// answers: every request pays the same dial timeout.
type countingRoundTripper struct {
	calls int
	err   error
}

func (t *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, t.err
}

func TestDefaultViewStopsAfterTransportFailure(t *testing.T) {
	client := testClient(t, "https://pve.example")
	transport := &countingRoundTripper{err: &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}}
	client.http.Transport = transport

	view, err := client.DefaultView(context.Background())
	if err != nil {
		t.Fatalf("DefaultView() error: %v", err)
	}
	if transport.calls != 1 {
		t.Fatalf("expected one request before DefaultView gave up, got %d", transport.calls)
	}
	if !view.CollectorFailed(CollectorVersion) || !view.CollectorFailed(CollectorCluster) || !view.CollectorFailed(CollectorResources) {
		t.Fatalf("expected every collector marked failed once the host is unreachable, got: %#v", view.Failed)
	}
	if Classify(view.FirstErr) != FailureTransport {
		t.Fatalf("FirstErr class = %q, want %q", Classify(view.FirstErr), FailureTransport)
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

func TestPostUsesSharedAuthenticatedTransport(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got, want := r.Header.Get("Authorization"), "PVEAPIToken=monitoring@pve!readonly=fixture-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"data":"UPID:fixture"}`))
	}))
	defer server.Close()

	var upid string
	if err := newTLSClient(t, server.URL, Options{Insecure: true}).post(context.Background(), "/api2/json/test", &upid); err != nil {
		t.Fatal(err)
	}
	if upid != "UPID:fixture" {
		t.Errorf("UPID = %q, want UPID:fixture", upid)
	}
}

func TestGuestActions(t *testing.T) {
	tests := []struct {
		guestType string
		action    GuestAction
	}{
		{guestType: "qemu", action: GuestActionStart},
		{guestType: "qemu", action: GuestActionShutdown},
		{guestType: "qemu", action: GuestActionReboot},
		{guestType: "lxc", action: GuestActionStart},
		{guestType: "lxc", action: GuestActionShutdown},
		{guestType: "lxc", action: GuestActionReboot},
	}

	for _, tt := range tests {
		t.Run(tt.guestType+"/"+string(tt.action), func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if got, want := r.Header.Get("Authorization"), "PVEAPIToken=monitoring@pve!readonly=fixture-token"; got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
				wantPath := "/api2/json/nodes/pve%2Fedge/" + tt.guestType + "/100/status/" + string(tt.action)
				if got := r.URL.EscapedPath(); got != wantPath {
					t.Errorf("path = %q, want %q", got, wantPath)
				}
				_, _ = w.Write([]byte(`{"data":"UPID:pve1:opaque"}`))
			}))
			defer server.Close()

			upid, err := newTLSClient(t, server.URL, Options{Insecure: true}).ActOnGuest(context.Background(), "pve/edge", tt.guestType, 100, tt.action)
			if err != nil || upid != "UPID:pve1:opaque" {
				t.Fatalf("ActOnGuest() = %q, %v", upid, err)
			}
			if calls != 1 {
				t.Errorf("requests = %d, want 1", calls)
			}
		})
	}
}

func TestGuestActionValidationMakesNoRequest(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client := newTLSClient(t, server.URL, Options{Insecure: true})

	tests := []struct {
		name      string
		node      string
		guestType string
		vmid      int
		action    GuestAction
	}{
		{name: "empty node", guestType: "qemu", vmid: 100, action: GuestActionStart},
		{name: "invalid type", node: "pve1", guestType: "vm", vmid: 100, action: GuestActionStart},
		{name: "VMID too small", node: "pve1", guestType: "qemu", vmid: minVMID - 1, action: GuestActionStart},
		{name: "VMID too large", node: "pve1", guestType: "qemu", vmid: maxVMID + 1, action: GuestActionStart},
		{name: "invalid action", node: "pve1", guestType: "qemu", vmid: 100, action: GuestAction("stop")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := client.ActOnGuest(context.Background(), tt.node, tt.guestType, tt.vmid, tt.action); err == nil {
				t.Fatal("ActOnGuest() error = nil")
			}
		})
	}
	for _, target := range []struct{ node, upid string }{
		{node: "", upid: "UPID:opaque"},
		{node: " ", upid: "UPID:opaque"},
		{node: "pve1", upid: ""},
		{node: "pve1", upid: " "},
	} {
		if _, err := client.TaskStatus(context.Background(), target.node, target.upid); err == nil {
			t.Errorf("TaskStatus(%q, %q) error = nil", target.node, target.upid)
		}
	}
	if calls != 0 {
		t.Errorf("requests = %d, want 0", calls)
	}
}

func TestGuestActionRejectsInvalidUPIDResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":""}`))
	}))
	defer server.Close()

	if _, err := newTLSClient(t, server.URL, Options{Insecure: true}).ActOnGuest(context.Background(), "pve1", "qemu", 100, GuestActionStart); err == nil || !strings.Contains(err.Error(), "empty UPID") {
		t.Errorf("ActOnGuest() error = %v", err)
	}
}

func TestTaskStatus(t *testing.T) {
	tests := []struct {
		fixture    string
		status     string
		exitStatus string
	}{
		{fixture: "task-status-running.json", status: "running"},
		{fixture: "task-status-stopped-ok.json", status: "stopped", exitStatus: "OK"},
		{fixture: "task-status-stopped-error.json", status: "stopped", exitStatus: "guest is locked"},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				if got, want := r.URL.EscapedPath(), "/api2/json/nodes/pve%2Fedge/tasks/UPID:pve1%2Fopaque%3Fvalue/status"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				_, _ = w.Write(readFixture(t, tt.fixture))
			}))
			defer server.Close()

			status, err := newTLSClient(t, server.URL, Options{Insecure: true}).TaskStatus(context.Background(), "pve/edge", "UPID:pve1/opaque?value")
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != tt.status || status.ExitStatus != tt.exitStatus || status.Result != TaskResult(tt.status, tt.exitStatus) || status.UPID == "" {
				t.Errorf("TaskStatus() = %#v", status)
			}
		})
	}
}

func TestGuestActionDoesNotRetryAmbiguousFailure(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.Close()
	}))
	defer server.Close()

	_, err := newTLSClient(t, server.URL, Options{Insecure: true}).ActOnGuest(context.Background(), "pve1", "qemu", 100, GuestActionStart)
	if err == nil {
		t.Fatal("ActOnGuest() error = nil")
	}
	if calls != 1 {
		t.Errorf("requests = %d, want 1", calls)
	}
	if strings.Contains(err.Error(), "fixture-token") {
		t.Errorf("error exposes token: %v", err)
	}
}

func TestGuestActionDoesNotFollowRedirect(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()

	sourceCalls := 0
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls++
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	_, err := newTLSClient(t, source.URL, Options{Insecure: true}).ActOnGuest(context.Background(), "pve1", "qemu", 100, GuestActionStart)
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("ActOnGuest() error = %v", err)
	}
	if sourceCalls != 1 || targetCalls != 0 {
		t.Errorf("requests = source:%d target:%d, want source:1 target:0", sourceCalls, targetCalls)
	}
}

func TestAPIErrorRedactsReflectedToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"credential fixture-token was denied"}`))
	}))
	defer server.Close()

	_, err := newTLSClient(t, server.URL, Options{Insecure: true}).ActOnGuest(context.Background(), "pve1", "qemu", 100, GuestActionStart)
	if err == nil || strings.Contains(err.Error(), "fixture-token") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("ActOnGuest() error = %v", err)
	}
}

func TestAPIErrorIncludesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(readFixture(t, "error-403-permission.json"))
	}))
	defer server.Close()

	_, err := testClient(t, server.URL).Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden") || !strings.Contains(err.Error(), "Permission check failed (/, Sys.Audit)") || Classify(err) != FailureAuthorization {
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
		if _, err := client.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") || Classify(err) != FailureTLS {
			t.Errorf("Version() error = %#v", err)
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

func TestFailureClassification(t *testing.T) {
	t.Run("CA file unreadable", func(t *testing.T) {
		// A missing or unreadable CA file is a certificate trust problem, not
		// a credential problem: the token is never even read. Classifying it
		// FailureAuthentication sent an operator staring at a fine token
		// while pointing at "the token may be missing, unreadable, or
		// revoked" instead of the ca_file setting that is actually wrong.
		_, err := New(Options{Host: "pve.example", TokenID: "id", Token: "secret", CAFile: "missing.pem"})
		if Classify(err) != FailureTLS {
			t.Fatalf("Classify(%v) = %q, want %q", err, Classify(err), FailureTLS)
		}
	})

	t.Run("fingerprint format", func(t *testing.T) {
		_, err := New(Options{Host: "pve.example", TokenID: "id", Token: "secret", Fingerprint: "not-hex"})
		if Classify(err) != FailureTLS {
			t.Fatalf("Classify(%v) = %q, want %q", err, Classify(err), FailureTLS)
		}
	})

	t.Run("TLS handshake", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		if _, err := newTLSClient(t, server.URL, Options{}).Version(context.Background()); Classify(err) != FailureTLS {
			t.Fatalf("Classify(%v) = %q, want %q", err, Classify(err), FailureTLS)
		}
	})

	for _, tt := range []struct {
		name   string
		status int
		class  FailureClass
	}{
		{name: "authentication", status: http.StatusUnauthorized, class: FailureAuthentication},
		{name: "authorization", status: http.StatusForbidden, class: FailureAuthorization},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "fixture-token denied", tt.status)
			}))
			defer server.Close()
			_, err := testClient(t, server.URL).Version(context.Background())
			if Classify(err) != tt.class || strings.Contains(err.Error(), "fixture-token") {
				t.Fatalf("Classify(%v) = %q, want %q", err, Classify(err), tt.class)
			}
		})
	}

	t.Run("transport", func(t *testing.T) {
		client := testClient(t, "https://pve.example")
		client.http = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		})}
		_, err := client.Version(context.Background())
		if Classify(err) != FailureTransport {
			t.Fatalf("Classify(%v) = %q, want %q", err, Classify(err), FailureTransport)
		}
	})

	t.Run("response read", func(t *testing.T) {
		client := testClient(t, "https://pve.example")
		client.http = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errReader{}}, nil
		})}
		_, err := client.Version(context.Background())
		if Classify(err) != FailureTransport {
			t.Fatalf("Classify(%v) = %q, want %q", err, Classify(err), FailureTransport)
		}
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReader) Close() error             { return nil }

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
