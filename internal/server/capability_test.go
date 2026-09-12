package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
)

func capabilityServer(demo bool) *Server {
	cfg := &config.Config{
		Servers: []config.ServerConfig{{Name: "myserver", Host: "192.168.1.10", Local: true}},
		Alerts:  config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
	return New(cfg, "127.0.0.1", 8080, demo)
}

// routes() registers from the registry and skips a capability it has no
// handler for, so without this the two drift apart quietly: the registry would
// promise a path and the mux would answer the SPA fallback.
func TestEveryExposedCapabilityHasAHandler(t *testing.T) {
	for _, demo := range []bool{false, true} {
		mode := "real"
		if demo {
			mode = "demo"
		}
		handlers := capabilityServer(demo).capabilityHandlers()

		for _, c := range capability.Registry {
			_, ok := handlers[c.Tool.Name]
			switch {
			case c.Exposed() && !ok:
				t.Errorf("%s mode: the registry exposes %s at %s %s and nothing answers for it",
					mode, c.Tool.Name, c.HTTP.Method, c.HTTP.Path)
			case !c.Exposed() && ok:
				t.Errorf("%s mode: %s has a handler but the registry says it is not exposed (%s)",
					mode, c.Tool.Name, c.HTTP.Absent)
			}
		}

		for name := range handlers {
			if _, ok := capability.For(name); !ok {
				t.Errorf("%s mode: handler registered for %q, which is not a capability", mode, name)
			}
		}
	}
}

// The paths the registry declares have to be the paths the mux answers on,
// which is the whole point of registering from it.
func TestExposedCapabilitiesAnswerOnTheirDeclaredPath(t *testing.T) {
	srv := capabilityServer(true)

	for _, c := range capability.Registry {
		if !c.Exposed() || c.HTTP.Method != "GET" {
			continue
		}
		req := httptest.NewRequest(c.HTTP.Method, c.HTTP.Path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s at %s answered %s, which means the SPA fallback took it",
				c.Tool.Name, c.HTTP.Path, ct)
		}
	}
}

func TestCapabilitiesEndpointReportsWhatIsAndIsNotReachable(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/capabilities", nil)
	w := httptest.NewRecorder()
	capabilityServer(false).Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got []capabilityInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(capability.Registry) {
		t.Fatalf("reported %d capabilities, the registry holds %d", len(got), len(capability.Registry))
	}

	var exposed, absent int
	for _, c := range got {
		if c.Risk == "" {
			t.Errorf("%s reported without a risk", c.Name)
		}
		if c.Method != "" {
			exposed++
			continue
		}
		absent++
		if c.Absent == "" {
			t.Errorf("%s is not reachable and the reason is missing", c.Name)
		}
	}
	if exposed == 0 || absent == 0 {
		t.Fatalf("expected both reachable and unreachable capabilities, got %d and %d", exposed, absent)
	}
}
