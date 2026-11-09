package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Higangssh/homebutler/internal/ports"
)

// On Windows /api/ports answered 200 with [], which the dashboard showed as
// "No open ports detected" for a machine it had not looked at (#384).
func TestPortsEndpointSaysWhenItCouldNotLook(t *testing.T) {
	old := listPorts
	listPorts = func() (*ports.Result, error) { return nil, errors.New("unsupported OS: windows") }
	defer func() { listPorts = old }()

	srv := testServer()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/ports", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not an error object: %s", w.Body.String())
	}
	if body["error"] != "unsupported OS: windows" {
		t.Errorf("error = %q, want the collector's reason", body["error"])
	}
}
