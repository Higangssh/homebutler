package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// A container cannot see the machine it runs on. Answering with its own /proc
// would be worse than failing: the numbers look right, they update, and they
// describe a machine nobody asked about.
func TestALocalServerIsNotAnsweredFromInsideAContainer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")

	cfg := &config.Config{Servers: []config.ServerConfig{{Name: "this-host", Host: "127.0.0.1", Local: true}}}
	srv := New(cfg, "127.0.0.1", 8080)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/overview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Servers []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
			System  any    `json:"system"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Servers) != 1 {
		t.Fatalf("expected one server, got %d", len(body.Servers))
	}

	reading := body.Servers[0]
	if reading.System != nil {
		t.Fatalf("the container answered for its host: %+v", reading.System)
	}
	if reading.Status != "unavailable" {
		t.Fatalf("status is %q, want unavailable", reading.Status)
	}
	// Both ways out, not just the problem: somebody who wanted their host
	// watched still has to end up doing one of them.
	for _, want := range []string{"cannot see the machine it is on", "running homebutler on it directly", "servers:"} {
		if !strings.Contains(reading.Message, want) {
			t.Fatalf("the message does not mention %q: %s", want, reading.Message)
		}
	}
}

func TestStatusSaysSoRatherThanReportingTheContainer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")

	srv := New(&config.Config{}, "127.0.0.1", 8080)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "cannot see the machine it is on") {
		t.Fatalf("the refusal does not say why: %s", w.Body)
	}
}

// Outside a container nothing changes: the local machine is the local machine.
func TestALocalServerIsAnsweredNormallyOutsideAContainer(t *testing.T) {
	cfg := &config.Config{Servers: []config.ServerConfig{{Name: "this-host", Host: "127.0.0.1", Local: true}}}
	srv := New(cfg, "127.0.0.1", 8080)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/overview", nil))

	var body struct {
		Servers []struct {
			Status string `json:"status"`
			System any    `json:"system"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Servers[0].System == nil {
		t.Fatalf("the local machine was not read: %+v", body.Servers[0])
	}
}
