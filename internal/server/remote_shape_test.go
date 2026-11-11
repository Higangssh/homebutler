package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

// remoteAnswering stubs the SSH hop with what the named command prints, and
// fails the test if the route forwards a different command.
func remoteAnswering(t *testing.T, want string, out []byte) *Server {
	t.Helper()
	return testServerWithMockRemote(func(_ *config.ServerConfig, args ...string) ([]byte, error) {
		if len(args) == 0 || args[0] != want {
			t.Errorf("forwarded %v, want %s", args, want)
		}
		return out, nil
	})
}

func getRoute(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

// strictDecode fails on a field the target type does not have, so a remote
// answer still wrapped in its CLI envelope cannot pass for the route's shape.
func strictDecode(t *testing.T, body []byte, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("answer is not the local route's shape: %v\nbody: %s", err, body)
	}
}

// The dashboard's Top Processes card was empty for every SSH server: the
// remote's `processes --json` is an object, and the card reads the list
// /api/processes answers with locally.
func TestRemoteProcessesAnswerTheLocalShape(t *testing.T) {
	// Built from the type the CLI prints, so this follows it if it grows.
	out, err := json.Marshal(system.ProcessResult{
		Processes: []system.ProcessInfo{
			{PID: 35224, Name: "sshd", CPU: 2.8, Mem: 0.1, RSS: 9000, State: "S"},
			{PID: 878, Name: "dockerd", CPU: 0, Mem: 1, RSS: 88284, State: "S"},
		},
		Total:   42,
		Zombies: []system.ProcessInfo{{PID: 9, Name: "defunct", Zombie: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := getRoute(t, remoteAnswering(t, "processes", out), "/api/processes?server=remote1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}

	var procs []system.ProcessInfo
	strictDecode(t, w.Body.Bytes(), &procs)
	if len(procs) != 2 || procs[0].Name != "sshd" || procs[1].PID != 878 {
		t.Fatalf("processes = %+v, want the remote's two in its order", procs)
	}
}

// A remote older than 0.12.0 printed the bare list, which was already right.
func TestRemoteProcessesFromAnOldRemotePassThrough(t *testing.T) {
	w := getRoute(t, remoteAnswering(t, "processes", []byte(`[{"pid":1,"name":"init","cpu":0.1,"mem":0.2,"rss":100}]`)),
		"/api/processes?server=remote1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	var procs []system.ProcessInfo
	strictDecode(t, w.Body.Bytes(), &procs)
	if len(procs) != 1 || procs[0].Name != "init" {
		t.Fatalf("processes = %+v, want the one the remote sent", procs)
	}
}

// An empty list is [] and not null, as it is for the local route.
func TestRemoteProcessesEmptyIsAList(t *testing.T) {
	w := getRoute(t, remoteAnswering(t, "processes", []byte(`{"processes":null,"total":0}`)), "/api/processes?server=remote1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

// A field a newer remote adds to each process is kept, not dropped by a round
// trip through this binary's struct.
func TestRemoteProcessesKeepFieldsThisBinaryDoesNotKnow(t *testing.T) {
	w := getRoute(t, remoteAnswering(t, "processes", []byte(`{"processes":[{"pid":1,"name":"init","cpu":0,"mem":0,"rss":1,"cgroup":"/init.scope"}],"total":1}`)),
		"/api/processes?server=remote1")
	var procs []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &procs); err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 || procs[0]["cgroup"] != "/init.scope" {
		t.Fatalf("processes = %v, want the remote's cgroup kept", procs)
	}
}

// An object that is not a process answer is still a bad gateway, not an
// empty table that reads as "nothing is running".
func TestRemoteProcessesRejectAnUnrelatedObject(t *testing.T) {
	w := getRoute(t, remoteAnswering(t, "processes", []byte(`{"hostname":"pi"}`)), "/api/processes?server=remote1")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", w.Code, w.Body.String())
	}
}

// The Network Ports card had the opposite problem: /api/ports answers
// {"ports": [...]} locally and the remote's `ports --json` is the bare list.
func TestRemotePortsAnswerTheLocalShape(t *testing.T) {
	// cmd/ports.go prints result.Ports, the list on its own.
	out, err := json.Marshal([]ports.PortInfo{
		{Protocol: "tcp", Address: "0.0.0.0", Port: "22"},
		{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Container: "web (nginx)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := getRoute(t, remoteAnswering(t, "ports", out), "/api/ports?server=remote1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}

	var result ports.Result
	strictDecode(t, w.Body.Bytes(), &result)
	if len(result.Ports) != 2 || result.Ports[1].Container != "web (nginx)" {
		t.Fatalf("ports = %+v, want the remote's two", result.Ports)
	}
}

func TestRemotePortsEmptyIsAList(t *testing.T) {
	for _, out := range []string{`[]`, `{"ports":null}`} {
		w := getRoute(t, remoteAnswering(t, "ports", []byte(out)), "/api/ports?server=remote1")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", out, w.Code)
		}
		if got := strings.TrimSpace(w.Body.String()); got != `{"ports":[]}` {
			t.Fatalf("%s: body = %s, want {\"ports\":[]}", out, got)
		}
	}
}

// A ports value that is not a list was passed through as a 200, so the card
// had an object where it reads a list.
func TestRemotePortsRejectAnUnrelatedObject(t *testing.T) {
	for _, out := range []string{`{"hostname":"pi"}`, `{"ports":{"a":1}}`, `{"ports":"22"}`} {
		w := getRoute(t, remoteAnswering(t, "ports", []byte(out)), "/api/ports?server=remote1")
		if w.Code != http.StatusBadGateway {
			t.Errorf("%s: status = %d, want 502; body %s", out, w.Code, w.Body.String())
		}
	}
}
