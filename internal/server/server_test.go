package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

func testServer() *Server {
	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "myserver", Host: "192.168.1.10", Local: true},
			{Name: "remote1", Host: "10.0.0.5"},
		},
		Wake: []config.WakeTarget{
			{Name: "test-pc", MAC: "AA:BB:CC:DD:EE:FF"},
		},
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
	return New(cfg, "127.0.0.1", 8080)
}

func testDemoServer() *Server {
	cfg := &config.Config{
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
	return New(cfg, "127.0.0.1", 8080, true)
}

func TestStatusEndpoint(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["hostname"]; !ok {
		t.Fatal("missing hostname field")
	}
	if _, ok := result["cpu"]; !ok {
		t.Fatal("missing cpu field")
	}
}

func TestProcessesEndpoint(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/processes", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least one process")
	}
	if _, ok := result[0]["name"]; !ok {
		t.Fatal("missing name field in process")
	}
}

func TestAlertsEndpoint(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/alerts", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["cpu"]; !ok {
		t.Fatal("missing cpu field in alerts")
	}
	if _, ok := result["memory"]; !ok {
		t.Fatal("missing memory field in alerts")
	}
}

func TestServersEndpoint(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/servers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(result))
	}
	if result[0]["name"] != "myserver" {
		t.Fatalf("expected server name 'myserver', got %v", result[0]["name"])
	}
}

func TestServerStatusLocalEndpoint(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/servers/myserver/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["hostname"]; !ok {
		t.Fatal("missing hostname field")
	}
}

func TestServerStatusNotFound(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/servers/nonexistent/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["error"]; !ok {
		t.Fatal("missing error field")
	}
}

func TestCORSHeaders(t *testing.T) {
	srv := testServer()

	// No Origin header → no CORS response
	req := httptest.NewRequest("GET", "/api/servers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("expected no CORS header without Origin, got %q", v)
	}

	// Allowed origin → CORS response
	req = httptest.NewRequest("GET", "/api/servers", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "http://127.0.0.1:8080" {
		t.Fatalf("expected CORS header http://127.0.0.1:8080, got %q", v)
	}

	// Unknown origin → no CORS response
	req = httptest.NewRequest("GET", "/api/servers", nil)
	req.Header.Set("Origin", "http://evil.com")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("expected no CORS header for unknown origin, got %q", v)
	}
}

func TestDockerEndpointReturnsJSON(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/docker", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Docker may return 200 (containers) or 500 (docker not installed), both should be valid JSON
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", w.Body.String())
	}
}

func TestFrontendFallback(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html, got %s", ct)
	}
	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected non-empty HTML body")
	}
}

func TestPortsEndpointReturnsJSON(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/ports", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", w.Body.String())
	}
}

func TestWakeListEndpoint(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/wake", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 wake target, got %d", len(result))
	}
	if result[0]["name"] != "test-pc" {
		t.Fatalf("expected wake target 'test-pc', got %v", result[0]["name"])
	}
}

// --- Demo mode tests ---

func TestDemoStatusEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["hostname"] != "homelab-server" {
		t.Fatalf("expected demo hostname 'homelab-server', got %v", result["hostname"])
	}
}

func TestDemoDockerEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/docker", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["available"] != true {
		t.Fatal("expected available=true in demo docker")
	}
	containers, ok := result["containers"].([]any)
	if !ok || len(containers) != 6 {
		t.Fatalf("expected 6 demo containers, got %v", len(containers))
	}
}

func TestDemoProcessesEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/processes", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 10 {
		t.Fatalf("expected 10 demo processes, got %d", len(result))
	}
}

func TestDemoAlertsEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/alerts", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	disks, ok := result["disks"].([]any)
	if !ok || len(disks) < 2 {
		t.Fatal("expected at least 2 demo disk alerts")
	}
	disk2 := disks[1].(map[string]any)
	if disk2["status"] != "warning" {
		t.Fatalf("expected /mnt/data disk to be 'warning', got %v", disk2["status"])
	}
}

func TestDemoPortsEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/ports", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 7 {
		t.Fatalf("expected 7 demo ports, got %d", len(result))
	}
}

func TestDemoWakeEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/wake", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 demo wake targets, got %d", len(result))
	}
}

func TestDemoWakeSendEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("POST", "/api/wake/nas-server", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["status"] != "sent" {
		t.Fatalf("expected status 'sent', got %v", result["status"])
	}
	if result["target"] != "nas-server" {
		t.Fatalf("expected target 'nas-server', got %v", result["target"])
	}
}

func TestDemoServersEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/servers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 10 {
		t.Fatalf("expected 10 demo servers, got %d", len(result))
	}
}

func TestDemoServerStatusEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/servers/nas-box/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["hostname"] != "nas-box" {
		t.Fatalf("expected hostname 'nas-box', got %v", result["hostname"])
	}
}

func TestDemoServerStatusNotFound(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/servers/nonexistent/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- Server switching tests (demo mode with ?server= param) ---

func TestDemoStatusWithServerParam(t *testing.T) {
	srv := testDemoServer()

	// nas-box should return different hostname and CPU
	req := httptest.NewRequest("GET", "/api/status?server=nas-box", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["hostname"] != "nas-box" {
		t.Fatalf("expected hostname 'nas-box', got %v", result["hostname"])
	}
	cpu := result["cpu"].(map[string]any)
	if cpu["usage_percent"] != 5.2 {
		t.Fatalf("expected nas-box CPU 5.2, got %v", cpu["usage_percent"])
	}
}

func TestDemoStatusWithLocalServerParam(t *testing.T) {
	srv := testDemoServer()

	// homelab-server (local) should return normal demo data
	req := httptest.NewRequest("GET", "/api/status?server=homelab-server", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["hostname"] != "homelab-server" {
		t.Fatalf("expected hostname 'homelab-server', got %v", result["hostname"])
	}
}

func TestDemoStatusWithOfflineServer(t *testing.T) {
	srv := testDemoServer()

	// backup-nas is "error" status - should return offline error
	req := httptest.NewRequest("GET", "/api/status?server=backup-nas", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestDemoDockerWithServerParam(t *testing.T) {
	srv := testDemoServer()

	req := httptest.NewRequest("GET", "/api/docker?server=nas-box", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	containers := result["containers"].([]any)
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers for nas-box, got %d", len(containers))
	}
}

func TestDemoDockerRaspberryPi(t *testing.T) {
	srv := testDemoServer()

	req := httptest.NewRequest("GET", "/api/docker?server=raspberry-pi", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	containers := result["containers"].([]any)
	if len(containers) != 1 {
		t.Fatalf("expected 1 container for raspberry-pi, got %d", len(containers))
	}
	c0 := containers[0].(map[string]any)
	if c0["name"] != "pihole" {
		t.Fatalf("expected container 'pihole', got %v", c0["name"])
	}
}

func TestDemoProcessesWithServerParam(t *testing.T) {
	srv := testDemoServer()

	req := httptest.NewRequest("GET", "/api/processes?server=nas-box", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 5 {
		t.Fatalf("expected 5 processes for nas-box, got %d", len(result))
	}
}

func TestDemoAlertsWithServerParam(t *testing.T) {
	srv := testDemoServer()

	req := httptest.NewRequest("GET", "/api/alerts?server=raspberry-pi", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	disks := result["disks"].([]any)
	if len(disks) != 1 {
		t.Fatalf("expected 1 disk alert for raspberry-pi, got %d", len(disks))
	}
}

func TestDemoPortsWithServerParam(t *testing.T) {
	srv := testDemoServer()

	req := httptest.NewRequest("GET", "/api/ports?server=nas-box", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 ports for nas-box, got %d", len(result))
	}
}

func TestDemoNoServerParamUnchanged(t *testing.T) {
	srv := testDemoServer()

	// Without ?server param, should return default homelab-server data
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["hostname"] != "homelab-server" {
		t.Fatalf("expected hostname 'homelab-server', got %v", result["hostname"])
	}
}

func TestRealServerNoServerParamUnchanged(t *testing.T) {
	srv := testServer()

	// Real mode without ?server= should work as before (local)
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["hostname"]; !ok {
		t.Fatal("missing hostname field")
	}
}

func TestRealServerLocalServerParam(t *testing.T) {
	srv := testServer()

	// ?server=myserver (local) should fall through to local handler
	req := httptest.NewRequest("GET", "/api/status?server=myserver", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["hostname"]; !ok {
		t.Fatal("missing hostname field")
	}
}
