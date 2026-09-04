package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/proxmox"
)

// proxmoxFixtureClient builds a Client that talks to a local TLS test server
// using an insecure connection, mirroring internal/server/proxmox_test.go.
func proxmoxFixtureClient(t *testing.T, server *httptest.Server) *proxmox.Client {
	t.Helper()
	apiURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(apiURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	client, err := proxmox.New(proxmox.Options{
		Host: host, Port: port, TokenID: "monitoring@pve!readonly", Token: "test-token", Insecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func resourcesResponse(nodes, guests string) string {
	items := []string{}
	if nodes != "" {
		items = append(items, nodes)
	}
	if guests != "" {
		items = append(items, guests)
	}
	return fmt.Sprintf(`{"data":[%s]}`, strings.Join(items, ","))
}

const oneNode = `{"type":"node","node":"pve1","status":"online"}`

func guestJSON(node, guestType string, vmid int, status string) string {
	return fmt.Sprintf(`{"type":%q,"vmid":%d,"node":%q,"status":%q}`, guestType, vmid, node, status)
}

// scriptedServer replays one response body per call to /cluster/resources, in
// order, then repeats the last one. status lets a call simulate an HTTP
// failure instead of a body.
type scriptedServer struct {
	mu        sync.Mutex
	responses []scriptedResponse
	calls     int32
}

type scriptedResponse struct {
	status int
	body   string
}

func (s *scriptedServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		i := int(atomic.AddInt32(&s.calls, 1)) - 1
		if i >= len(s.responses) {
			i = len(s.responses) - 1
		}
		resp := s.responses[i]
		s.mu.Unlock()
		if resp.status != 0 && resp.status != http.StatusOK {
			http.Error(w, "scripted failure", resp.status)
			return
		}
		_, _ = w.Write([]byte(resp.body))
	}
}

// runPolls drives n synchronous polls of a ProxmoxMonitor without going
// through the timer-driven Watch loop, so a test controls exactly which
// poll the incident channel should be checked after.
func runPolls(t *testing.T, pm *ProxmoxMonitor, n int) []Incident {
	t.Helper()
	ctx := context.Background()
	endpointPrev := make(map[string]proxmoxEndpointState)
	guestPrev := make(map[string]bool)
	incCh := make(chan Incident, 64)

	// Seed pass, matching Watch's own behavior.
	if err := pm.poll(ctx, endpointPrev, guestPrev, nil); err != nil {
		t.Fatalf("seed poll: %v", err)
	}

	var got []Incident
	for i := 0; i < n; i++ {
		if err := pm.poll(ctx, endpointPrev, guestPrev, incCh); err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
	}
	close(incCh)
	for inc := range incCh {
		got = append(got, inc)
	}
	return got
}

func TestProxmoxMonitorGuestDownAndRecovered(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "stopped"))},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{
		Endpoint: "pve",
		Client:   proxmoxFixtureClient(t, server),
		Guests:   []proxmox.ExpectedGuest{{Node: "pve1", Type: "qemu", VMID: 100}},
	}}}

	got := runPolls(t, pm, 2)
	if len(got) != 2 {
		t.Fatalf("incidents = %d, want 2: %+v", len(got), got)
	}
	if got[0].Source != "proxmox" || got[0].ProxmoxState != ProxmoxStateGuestDown || got[0].Recovered {
		t.Errorf("first incident = %+v, want unrecovered guest_down", got[0])
	}
	if !got[1].Recovered || got[1].ProxmoxState != ProxmoxStateGuestDown {
		t.Errorf("second incident = %+v, want recovered guest_down", got[1])
	}
}

func TestProxmoxMonitorUnconfiguredGuestNeverAlerts(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, guestJSON("pve1", "lxc", 200, "running"))},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "lxc", 200, "stopped"))},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	// Guest 200 is never declared in Targets[0].Guests, so it is
	// observational only.
	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{
		Endpoint: "pve",
		Client:   proxmoxFixtureClient(t, server),
	}}}

	got := runPolls(t, pm, 1)
	if len(got) != 0 {
		t.Fatalf("incidents = %+v, want none for an unconfigured guest", got)
	}
}

func TestProxmoxMonitorEndpointUnavailableAndRecovered(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, "")},
		{status: http.StatusServiceUnavailable},
		{body: resourcesResponse(oneNode, "")},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{Endpoint: "pve", Client: proxmoxFixtureClient(t, server)}}}

	got := runPolls(t, pm, 2)
	if len(got) != 2 {
		t.Fatalf("incidents = %d, want 2: %+v", len(got), got)
	}
	if got[0].ProxmoxState != ProxmoxStateUnavailable || got[0].ProxmoxClass != string(proxmox.FailureTransport) || got[0].Recovered {
		t.Errorf("first incident = %+v, want unrecovered unavailable/transport", got[0])
	}
	if !got[1].Recovered || got[1].ProxmoxState != ProxmoxStateUnavailable {
		t.Errorf("second incident = %+v, want recovered unavailable", got[1])
	}
}

func TestProxmoxMonitorACLFilteredEmptyResult(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, "")},
		{body: `{"data":[]}`}, // authenticated but nothing visible at all
		{body: resourcesResponse(oneNode, "")},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{Endpoint: "pve", Client: proxmoxFixtureClient(t, server)}}}

	got := runPolls(t, pm, 2)
	if len(got) != 2 {
		t.Fatalf("incidents = %d, want 2: %+v", len(got), got)
	}
	if got[0].ProxmoxState != ProxmoxStateACLFiltered || got[0].ProxmoxClass != ProxmoxClassEmptyResult {
		t.Errorf("first incident = %+v, want acl_filtered/empty_result", got[0])
	}
	if !got[1].Recovered || got[1].ProxmoxState != ProxmoxStateACLFiltered {
		t.Errorf("second incident = %+v, want recovered acl_filtered", got[1])
	}
}

func TestProxmoxMonitorAuthorizationFailureIsACLFiltered(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, "")},
		{status: http.StatusForbidden},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{Endpoint: "pve", Client: proxmoxFixtureClient(t, server)}}}

	got := runPolls(t, pm, 1)
	if len(got) != 1 {
		t.Fatalf("incidents = %d, want 1: %+v", len(got), got)
	}
	if got[0].ProxmoxState != ProxmoxStateACLFiltered || got[0].ProxmoxClass != string(proxmox.FailureAuthorization) {
		t.Errorf("incident = %+v, want acl_filtered/authorization", got[0])
	}
}

func TestProxmoxMonitorSeedDoesNotAlertOnAlreadyStoppedGuest(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "stopped"))},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "stopped"))},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{
		Endpoint: "pve",
		Client:   proxmoxFixtureClient(t, server),
		Guests:   []proxmox.ExpectedGuest{{Node: "pve1", Type: "qemu", VMID: 100}},
	}}}

	got := runPolls(t, pm, 1)
	if len(got) != 0 {
		t.Fatalf("incidents = %+v, want none: a guest already stopped when watch starts is not a transition", got)
	}
}

// A failed endpoint collector must not erase guest state from a prior
// successful poll (#104 "preserve partial results").
func TestProxmoxMonitorPartialFailurePreservesGuestState(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
		{status: http.StatusServiceUnavailable},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "stopped"))},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{
		Endpoint: "pve",
		Client:   proxmoxFixtureClient(t, server),
		Guests:   []proxmox.ExpectedGuest{{Node: "pve1", Type: "qemu", VMID: 100}},
	}}}

	got := runPolls(t, pm, 2)

	var guestIncidents int
	for _, inc := range got {
		if inc.ProxmoxState == ProxmoxStateGuestDown {
			guestIncidents++
		}
	}
	if guestIncidents != 1 {
		t.Fatalf("guest_down incidents = %d, want exactly 1 (from running to stopped, not lost by the intervening failure): %+v", guestIncidents, got)
	}
}

func TestProxmoxMonitorACLFilteredEmptyResultPreservesGuestState(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
		{body: `{"data":[]}`},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{
		Endpoint: "pve", Client: proxmoxFixtureClient(t, server),
		Guests: []proxmox.ExpectedGuest{{Node: "pve1", Type: "qemu", VMID: 100}},
	}}}
	got := runPolls(t, pm, 2)
	if len(got) != 2 || got[0].ProxmoxState != ProxmoxStateACLFiltered || got[1].ProxmoxState != ProxmoxStateACLFiltered || !got[1].Recovered {
		t.Fatalf("incidents = %+v, want only ACL-filtered transition and recovery", got)
	}
}

func TestProxmoxMonitorMissingGuestPreservesState(t *testing.T) {
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
		{body: resourcesResponse(oneNode, "")},
		{body: resourcesResponse(oneNode, guestJSON("pve1", "qemu", 100, "running"))},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{Targets: []ProxmoxTarget{{
		Endpoint: "pve", Client: proxmoxFixtureClient(t, server),
		Guests: []proxmox.ExpectedGuest{{Node: "pve1", Type: "qemu", VMID: 100}},
	}}}
	if got := runPolls(t, pm, 2); len(got) != 0 {
		t.Fatalf("incidents = %+v, want none when a configured guest is absent", got)
	}
}

func TestProxmoxMonitorNoTargetsBlocksUntilCancelled(t *testing.T) {
	pm := &ProxmoxMonitor{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := pm.Watch(ctx, make(chan Incident)); err != context.DeadlineExceeded {
		t.Fatalf("Watch() = %v, want context.DeadlineExceeded", err)
	}
}

func TestProxmoxMonitorSavesIncidentsToDisk(t *testing.T) {
	dir := t.TempDir()
	script := &scriptedServer{responses: []scriptedResponse{
		{body: resourcesResponse(oneNode, "")},
		{status: http.StatusServiceUnavailable},
	}}
	server := httptest.NewTLSServer(script.handler(t))
	defer server.Close()

	pm := &ProxmoxMonitor{
		Targets: []ProxmoxTarget{{Endpoint: "pve", Client: proxmoxFixtureClient(t, server)}},
		Dir:     dir,
	}
	got := runPolls(t, pm, 1)
	if len(got) != 1 {
		t.Fatalf("incidents = %+v, want 1", got)
	}

	loaded, err := LoadIncident(dir, got[0].ID)
	if err != nil {
		t.Fatalf("LoadIncident: %v", err)
	}
	data, _ := json.Marshal(loaded)
	if loaded.ProxmoxState != ProxmoxStateUnavailable {
		t.Errorf("saved incident = %s, want proxmox_state unavailable", data)
	}
}
