package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

const testConfigFile = `servers:
  - name: local
    host: 127.0.0.1
    local: true
alerts:
  cpu: 90
  memory: 85
  disk: 90
`

// writableServer is a serve that can actually write: a real file on disk and a
// token, because the write routes do not exist without one.
func writableServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)
	srv.SetToken("test-token")
	return srv, path
}

func do(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// currentRevision is what a page holds: the token the dashboard was handed,
// not anything computed from the file.
func currentRevision(t *testing.T, srv *Server) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(do(t, srv, "GET", "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	rev, _ := body["revision"].(string)
	if rev == "" {
		t.Fatal("the config response carried no revision")
	}
	return rev
}

// TestSaveAndReadConcurrently is the test the race needed. A save replaces the
// config the process holds while requests are reading it, and nothing but
// concurrent traffic makes that visible: the rest of the suite sends one
// request at a time and stayed green through the bug.
func TestSaveAndReadConcurrently(t *testing.T) {
	srv, _ := writableServer(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			rev := currentRevision(t, srv)
			do(t, srv, "PUT", "/api/config/alerts", fmt.Sprintf(`{"revision":%q,"cpu":%d}`, rev, 70+i%20))
		}(i)
		go func() {
			defer wg.Done()
			for _, path := range []string{"/api/config", "/api/overview", "/api/servers", "/api/wake"} {
				do(t, srv, "GET", path, "")
			}
		}()
	}
	wg.Wait()

	// Whichever save won, the process and the file agree afterwards.
	var body map[string]any
	if err := json.Unmarshal(do(t, srv, "GET", "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	served := body["alerts"].(map[string]any)["cpu"].(float64)
	onDisk, err := config.Load(srv.config().Path)
	if err != nil {
		t.Fatal(err)
	}
	if served != onDisk.Alerts.CPU {
		t.Fatalf("serve reports cpu %v, the file says %v", served, onDisk.Alerts.CPU)
	}
}

func TestSaveRejectsARevisionFromAnotherProcess(t *testing.T) {
	srv, path := writableServer(t)
	stale := currentRevision(t, srv)

	// A restarted serve reading the same unchanged file. The page is looking
	// at current content, but its token was signed by a key that is gone.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(cfg, "127.0.0.1", 8080)
	restarted.SetToken("test-token")

	if got := currentRevision(t, restarted); got == stale {
		t.Fatal("two processes issued the same revision token, so it is not keyed per process")
	}
	if w := do(t, restarted, "PUT", "/api/config/alerts", fmt.Sprintf(`{"revision":%q,"cpu":75}`, stale)); w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a token from another process, got %d: %s", w.Code, w.Body)
	}
}

// The revision is a handle on the file's contents, so a dashboard with no way
// to send it back is not given one.
func TestReadOnlyDashboardGetsNoRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["editable"] != false {
		t.Fatalf("a dashboard without a token reported editable %v", body["editable"])
	}
	if _, ok := body["revision"]; ok {
		t.Fatalf("a read-only dashboard was handed a revision: %v", body["revision"])
	}
}

func TestSaveRefusesAnOversizedBody(t *testing.T) {
	srv, _ := writableServer(t)
	rev := currentRevision(t, srv)
	body := fmt.Sprintf(`{"revision":%q,"cpu":75,"padding":%q}`, rev, strings.Repeat("x", maxSaveRequest+1))

	if w := do(t, srv, "PUT", "/api/config/alerts", body); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body)
	}
}
