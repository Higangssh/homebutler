package server

import (
	"encoding/json"
	"fmt"
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

// --- Docker stats endpoint ---

func TestDockerStatsEndpointReturnsJSON(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/docker/stats", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", w.Body.String())
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if _, ok := result["available"]; !ok {
		t.Fatal("missing 'available' key")
	}
	if _, ok := result["stats"]; !ok {
		t.Fatal("missing 'stats' key")
	}
}

func TestDockerStatsEndpoint_RemoteSuccess(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`[{"id":"abc","name":"nginx","cpu_percent":"0.50%","mem_usage":"10MiB / 1GiB","mem_percent":"0.53%","net_io":"1kB / 2kB","block_io":"0B / 0B","pids":"2"}]`), nil
	})
	req := httptest.NewRequest("GET", "/api/docker/stats?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}
	stats, ok := result["stats"].([]any)
	if !ok {
		t.Fatal("stats should be array")
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
}

func TestDockerStatsEndpoint_RemoteError(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("docker not available")
	})
	req := httptest.NewRequest("GET", "/api/docker/stats?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
}

func TestDockerStatsEndpoint_RemoteInvalidJSON(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte("not json"), nil
	})
	req := httptest.NewRequest("GET", "/api/docker/stats?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != false {
		t.Fatalf("expected available=false for invalid JSON, got %v", result["available"])
	}
}

// --- Demo docker stats ---

func TestDemoDockerStatsEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/docker/stats", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != true {
		t.Fatal("expected available=true in demo docker stats")
	}
	stats, ok := result["stats"].([]any)
	if !ok || len(stats) != 5 {
		t.Fatalf("expected 5 demo stats, got %v", len(stats))
	}
}

func TestDemoDockerStatsNasBox(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/docker/stats?server=nas-box", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	stats := result["stats"].([]any)
	if len(stats) != 2 {
		t.Fatalf("expected 2 stats for nas-box, got %d", len(stats))
	}
}

func TestDemoDockerStatsOfflineServer(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/docker/stats?server=backup-nas", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
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

// --- CORS handling tests ---

func TestCORS_LocalhostOriginAllowed(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "http://localhost:8080" {
		t.Fatalf("expected CORS for localhost, got %q", v)
	}
	if v := w.Header().Get("Access-Control-Allow-Methods"); v != "GET, POST, OPTIONS" {
		t.Fatalf("expected methods header, got %q", v)
	}
	if v := w.Header().Get("Access-Control-Allow-Headers"); v != "Content-Type, Authorization" {
		t.Fatalf("expected headers header, got %q", v)
	}
}

func TestCORS_HostOriginAllowed(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "http://127.0.0.1:8080" {
		t.Fatalf("expected CORS for 127.0.0.1:8080, got %q", v)
	}
}

func TestCORS_DifferentPortRejected(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Origin", "http://127.0.0.1:9090")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("expected no CORS for different port, got %q", v)
	}
}

// --- OPTIONS handler tests ---

func TestOptionsHandler_AllowedOrigin(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("OPTIONS", "/api/status", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "http://127.0.0.1:8080" {
		t.Fatalf("expected CORS header, got %q", v)
	}
}

func TestOptionsHandler_RejectedOrigin(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("OPTIONS", "/api/status", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("expected no CORS header for evil origin, got %q", v)
	}
}

func TestOptionsHandler_NoOrigin(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("OPTIONS", "/api/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("expected no CORS header without Origin, got %q", v)
	}
}

// --- Version endpoint ---

func TestVersionEndpoint(t *testing.T) {
	srv := testServer()
	srv.SetVersion("1.2.3")
	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["version"] != "1.2.3" {
		t.Fatalf("expected version 1.2.3, got %s", result["version"])
	}
}

func TestVersionEndpointDefaultVersion(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["version"] != "dev" {
		t.Fatalf("expected default version 'dev', got %s", result["version"])
	}
}

// --- Config endpoint ---

func TestConfigEndpoint(t *testing.T) {
	cfg := &config.Config{
		Path: "/test/config.yaml",
		Servers: []config.ServerConfig{
			{Name: "local", Host: "127.0.0.1", Local: true},
			{Name: "remote", Host: "10.0.0.1", User: "admin", Port: 2222, AuthMode: "password", Password: "secret", KeyFile: "/home/user/.ssh/id_rsa"},
		},
		Wake: []config.WakeTarget{
			{Name: "pc", MAC: "AA:BB:CC:DD:EE:FF", Broadcast: "192.168.1.255"},
		},
		Alerts: config.AlertConfig{CPU: 80, Memory: 70, Disk: 85},
	}
	srv := New(cfg, "127.0.0.1", 8080)
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["path"] != "/test/config.yaml" {
		t.Fatalf("expected config path, got %v", result["path"])
	}
	servers := result["servers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	// Password should be masked
	remote := servers[1].(map[string]any)
	if remote["password"] != "••••••" {
		t.Fatalf("expected masked password, got %v", remote["password"])
	}
	if remote["auth"] != "password" {
		t.Fatalf("expected auth 'password', got %v", remote["auth"])
	}
	// Key should be basename only
	if remote["key"] != "id_rsa" {
		t.Fatalf("expected key 'id_rsa', got %v", remote["key"])
	}
	// Wake targets
	wakes := result["wake"].([]any)
	if len(wakes) != 1 {
		t.Fatalf("expected 1 wake target, got %d", len(wakes))
	}
	// Alerts
	alerts := result["alerts"].(map[string]any)
	if alerts["cpu"].(float64) != 80 {
		t.Fatalf("expected CPU alert 80, got %v", alerts["cpu"])
	}
}

func TestConfigEndpoint_EmptyPath(t *testing.T) {
	cfg := &config.Config{Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90}}
	srv := New(cfg, "127.0.0.1", 8080)
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["path"] != "(defaults)" {
		t.Fatalf("expected '(defaults)' for empty config path, got %v", result["path"])
	}
}

// --- Wake send endpoint ---

func TestWakeSendNotFound(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("POST", "/api/wake/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if _, ok := result["error"]; !ok {
		t.Fatal("expected error field")
	}
}

// --- isRemoteRequest tests ---

func TestIsRemoteRequest_NoParam(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/status", nil)
	s, ok := srv.isRemoteRequest(req)
	if ok || s != nil {
		t.Fatal("expected false for no server param")
	}
}

func TestIsRemoteRequest_LocalServer(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/status?server=myserver", nil)
	s, ok := srv.isRemoteRequest(req)
	if ok || s != nil {
		t.Fatal("expected false for local server")
	}
}

func TestIsRemoteRequest_RemoteServer(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/status?server=remote1", nil)
	s, ok := srv.isRemoteRequest(req)
	if !ok || s == nil {
		t.Fatal("expected true for remote server")
	}
	if s.Host != "10.0.0.5" {
		t.Fatalf("expected host 10.0.0.5, got %s", s.Host)
	}
}

func TestIsRemoteRequest_UnknownServer(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/status?server=unknown", nil)
	s, ok := srv.isRemoteRequest(req)
	if ok || s != nil {
		t.Fatal("expected false for unknown server")
	}
}

// --- Docker endpoint wrapping (bug #21) ---

func TestDockerEndpoint_LocalUnavailable(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/docker", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	// Should always have available and containers keys
	if _, ok := result["available"]; !ok {
		t.Fatal("missing 'available' key")
	}
	if _, ok := result["containers"]; !ok {
		t.Fatal("missing 'containers' key")
	}
}

// --- Ports endpoint ---

func TestPortsEndpoint_LocalReturnsArray(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/api/ports", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Should be valid JSON array
	if !json.Valid(w.Body.Bytes()) {
		t.Fatal("response is not valid JSON")
	}
}

// --- writeError / writeJSON ---

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadGateway, "test error")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["error"] != "test error" {
		t.Fatalf("expected 'test error', got %s", result["error"])
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["key"] != "value" {
		t.Fatalf("expected 'value', got %s", result["key"])
	}
}

// --- Server New defaults ---

func TestNew_DefaultHost(t *testing.T) {
	cfg := &config.Config{}
	srv := New(cfg, "", 8080)
	if srv.host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %s", srv.host)
	}
}

func TestNew_DemoMode(t *testing.T) {
	cfg := &config.Config{}
	srv := New(cfg, "0.0.0.0", 3000, true)
	if !srv.demo {
		t.Fatal("expected demo mode to be true")
	}
	if srv.port != 3000 {
		t.Fatalf("expected port 3000, got %d", srv.port)
	}
}

// --- Demo config endpoint ---

func TestDemoConfigEndpoint(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["path"] == nil {
		t.Fatal("expected path field in config")
	}
}

// --- Demo version endpoint (shared, not demo-specific) ---

func TestDemoVersionEndpoint(t *testing.T) {
	srv := testDemoServer()
	srv.SetVersion("0.9.0")
	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["version"] != "0.9.0" {
		t.Fatalf("expected version 0.9.0, got %s", result["version"])
	}
}

// --- Demo mode server-specific tests for remaining branches ---

func TestDemoDockerWithOfflineServer(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/docker?server=backup-nas", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Offline server should return 502
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestDemoProcessesWithOfflineServer(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/processes?server=backup-nas", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestDemoAlertsWithOfflineServer(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/alerts?server=backup-nas", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestDemoPortsWithOfflineServer(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/ports?server=backup-nas", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestDemoProcessesRaspberryPi(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/processes?server=raspberry-pi", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result) != 4 {
		t.Fatalf("expected 4 processes for raspberry-pi, got %d", len(result))
	}
}

func TestDemoPortsRaspberryPi(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/ports?server=raspberry-pi", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result) != 3 {
		t.Fatalf("expected 3 ports for raspberry-pi, got %d", len(result))
	}
}

func TestDemoAlertsNasBox(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/alerts?server=nas-box", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	disks := result["disks"].([]any)
	if len(disks) != 2 {
		t.Fatalf("expected 2 disk alerts for nas-box, got %d", len(disks))
	}
}

func TestDemoStatusRaspberryPi(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/status?server=raspberry-pi", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["hostname"] != "raspberry-pi" {
		t.Fatalf("expected hostname raspberry-pi, got %v", result["hostname"])
	}
}

func TestDemoDockerRaspberryPi_Detailed(t *testing.T) {
	srv := testDemoServer()
	req := httptest.NewRequest("GET", "/api/docker?server=raspberry-pi", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != true {
		t.Fatal("expected available=true")
	}
}

// --- Remote handler tests with mock runner ---

func testServerWithMockRemote(runner RemoteRunner) *Server {
	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "local", Host: "127.0.0.1", Local: true},
			{Name: "remote1", Host: "10.0.0.5"},
		},
		Wake:   []config.WakeTarget{{Name: "pc", MAC: "AA:BB:CC:DD:EE:FF"}},
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
	s := New(cfg, "127.0.0.1", 8080)
	s.remoteRunner = runner
	return s
}

func TestForwardRemote_Success(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`{"hostname":"remote-host","cpu":{"usage_percent":12.5}}`), nil
	})
	req := httptest.NewRequest("GET", "/api/status?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["hostname"] != "remote-host" {
		t.Fatalf("expected hostname 'remote-host', got %v", result["hostname"])
	}
}

func TestForwardRemote_Error(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("SSH connection refused")
	})
	req := httptest.NewRequest("GET", "/api/status?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["error"] == nil {
		t.Fatal("expected error in response")
	}
}

func TestForwardRemote_InvalidJSON(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte("this is not json"), nil
	})
	req := httptest.NewRequest("GET", "/api/status?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["error"] != "invalid response from remote server" {
		t.Fatalf("expected 'invalid response from remote server', got %v", result["error"])
	}
}

func TestHandleDocker_RemoteSuccess_WrapsResponse(t *testing.T) {
	// Bug #21: remote docker response should be wrapped with available/containers
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`[{"id":"abc","name":"nginx","state":"running"}]`), nil
	})
	req := httptest.NewRequest("GET", "/api/docker?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}
	containers, ok := result["containers"]
	if !ok {
		t.Fatal("missing containers key")
	}
	arr, ok := containers.([]any)
	if !ok {
		t.Fatalf("containers should be array, got %T", containers)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 container, got %d", len(arr))
	}
}

func TestHandleDocker_RemoteError_ReturnsUnavailable(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("docker not installed")
	})
	req := httptest.NewRequest("GET", "/api/docker?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
	if result["message"] != "docker not installed" {
		t.Fatalf("expected error message, got %v", result["message"])
	}
}

func TestHandleDocker_RemoteInvalidJSON(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte("not json"), nil
	})
	req := httptest.NewRequest("GET", "/api/docker?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["available"] != false {
		t.Fatalf("expected available=false for invalid JSON, got %v", result["available"])
	}
}

func TestHandleProcesses_RemoteSuccess(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`[{"name":"nginx","pid":1234,"cpu":1.2}]`), nil
	})
	req := httptest.NewRequest("GET", "/api/processes?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleProcesses_RemoteError(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("connection refused")
	})
	req := httptest.NewRequest("GET", "/api/processes?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestHandleAlerts_RemoteSuccess(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`{"cpu":{"status":"ok"}}`), nil
	})
	req := httptest.NewRequest("GET", "/api/alerts?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandlePorts_RemoteSuccess(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`[{"protocol":"tcp","port":"80"}]`), nil
	})
	req := httptest.NewRequest("GET", "/api/ports?server=remote1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleServerStatus_RemoteSuccess(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte(`{"hostname":"remote1","uptime":"5d"}`), nil
	})
	req := httptest.NewRequest("GET", "/api/servers/remote1/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["hostname"] != "remote1" {
		t.Fatalf("expected hostname remote1, got %v", result["hostname"])
	}
}

func TestHandleServerStatus_RemoteError(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("connection failed")
	})
	req := httptest.NewRequest("GET", "/api/servers/remote1/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestHandleServerStatus_RemoteInvalidJSON(t *testing.T) {
	srv := testServerWithMockRemote(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return []byte("not json"), nil
	})
	req := httptest.NewRequest("GET", "/api/servers/remote1/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

// --- SPA fallback ---

func TestSPAFallback_UnknownPath(t *testing.T) {
	srv := testServer()
	req := httptest.NewRequest("GET", "/some/unknown/path", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Should serve index.html (or fallback HTML) with 200
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", w.Code)
	}
}

func testServerWithToken(token string) *Server {
	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "myserver", Host: "192.168.1.10", Local: true},
		},
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
	s := New(cfg, "127.0.0.1", 8080)
	s.SetToken(token)
	return s
}

func TestTokenAuth_NoTokenConfigured(t *testing.T) {
	srv := testServer() // no token set
	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without token config, got %d", w.Code)
	}
}

func TestTokenAuth_ValidToken(t *testing.T) {
	srv := testServerWithToken("secret123")
	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", w.Code)
	}
}

func TestTokenAuth_MissingToken(t *testing.T) {
	srv := testServerWithToken("secret123")
	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}
}

func TestTokenAuth_WrongToken(t *testing.T) {
	srv := testServerWithToken("secret123")
	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestTokenAuth_NonAPIPath(t *testing.T) {
	srv := testServerWithToken("secret123")
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Non-API paths should not require auth
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-API path, got %d", w.Code)
	}
}

func TestTokenAuth_StatusEndpoint(t *testing.T) {
	srv := testServerWithToken("mytoken")
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer mytoken")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token on /api/status, got %d", w.Code)
	}
}
