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

// Every route that reads this machine refuses inside a container the way
// /api/status does, whether it names no server or one marked local: true.
// /api/servers/{name}/status answered a local server with the container's own
// CPU, memory and disks under the server's name.
func TestEveryLocalReadingRefusesLikeStatusInsideAContainer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")

	ran := false
	srv := testServerWithMockRemote(func(*config.ServerConfig, ...string) ([]byte, error) {
		ran = true
		return []byte(`{}`), nil
	})
	srv.SetToken("secret")

	serve := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	want := serve("GET", "/api/status")
	if want.Code != http.StatusNotImplemented {
		t.Fatalf("/api/status: expected 501, got %d: %s", want.Code, want.Body)
	}

	for _, route := range []struct{ method, path string }{
		{"GET", "/api/servers/local/status"},
		{"GET", "/api/status?server=local"},
		{"GET", "/api/processes"},
		{"GET", "/api/processes?server=local"},
		{"GET", "/api/alerts"},
		{"GET", "/api/alerts?server=local"},
		{"GET", "/api/ports"},
		{"GET", "/api/ports?server=local"},
		{"GET", "/api/report"},
		{"GET", "/api/report?server=local"},
		{"POST", "/api/report/snapshot"},
		{"POST", "/api/report/snapshot?server=local"},
		{"GET", "/api/doctor"},
		{"GET", "/api/doctor?server=local"},
	} {
		got := serve(route.method, route.path)
		if got.Code != want.Code || got.Body.String() != want.Body.String() {
			t.Errorf("%s %s: got %d %s, want what /api/status gives: %d %s",
				route.method, route.path, got.Code, got.Body, want.Code, want.Body)
		}
	}
	if ran {
		t.Error("a local server was sent over SSH")
	}
}

// The refusal is about this machine only: a server reached over SSH is a
// real machine, and a container can reach it as well as anything can.
func TestAContainerStillAnswersForARemoteServer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")

	srv := testServerWithMockRemote(func(*config.ServerConfig, ...string) ([]byte, error) {
		return []byte(`{"hostname":"remote-host"}`), nil
	})
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/servers/remote1/status", nil))

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "remote-host") {
		t.Fatalf("expected the remote's answer, got %d: %s", w.Code, w.Body)
	}
}
