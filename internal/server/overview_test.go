package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

func overviewServer(runner RemoteRunner, servers ...config.ServerConfig) *Server {
	cfg := &config.Config{Servers: servers, Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90}}
	s := New(cfg, "127.0.0.1", 8080)
	s.remoteRunner = runner
	return s
}

func getOverview(t *testing.T, srv *Server) overviewResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/overview", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got overviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func remotes(names ...string) []config.ServerConfig {
	out := make([]config.ServerConfig, len(names))
	for i, n := range names {
		out[i] = config.ServerConfig{Name: n, Host: "192.168.1." + fmt.Sprint(10+i)}
	}
	return out
}

// The card this replaces fetched each server in sequence, so the wait was the
// sum. Collecting in parallel is the reason the endpoint exists, and a test
// that only checked the output would pass on the serial version too.
func TestOverviewCollectsInParallel(t *testing.T) {
	const perServer = 200 * time.Millisecond
	var inFlight, peak int64

	srv := overviewServer(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		n := atomic.AddInt64(&inFlight, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
				break
			}
		}
		time.Sleep(perServer)
		atomic.AddInt64(&inFlight, -1)
		return []byte(`{"hostname":"h","uptime":"1d"}`), nil
	}, remotes("a", "b", "c", "d")...)

	start := time.Now()
	got := getOverview(t, srv)
	elapsed := time.Since(start)

	if len(got.Servers) != 4 {
		t.Fatalf("expected 4 readings, got %d", len(got.Servers))
	}
	if elapsed > 3*perServer {
		t.Fatalf("four servers took %s; %s each in sequence is what this replaced", elapsed, perServer)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency was %d: the collection is still serial", peak)
	}
}

// A reading that worked once is worth showing again with its age attached.
// Dropping it would turn a host that is briefly unreachable into a blank card.
func TestOverviewFallsBackToTheLastReading(t *testing.T) {
	var fail atomic.Bool
	srv := overviewServer(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		if fail.Load() {
			return nil, &remote.Error{Class: remote.ClassUnreachable, Err: fmt.Errorf("dial tcp 192.168.1.10:22: no route to host")}
		}
		return []byte(`{"hostname":"nas","uptime":"12d 3h"}`), nil
	}, remotes("nas")...)

	first := getOverview(t, srv).Servers[0]
	if first.Status != "current" || first.System == nil {
		t.Fatalf("expected a current reading, got %+v", first)
	}

	fail.Store(true)
	second := getOverview(t, srv).Servers[0]

	if second.Status != "stale" {
		t.Fatalf("expected stale, got %q", second.Status)
	}
	if second.System == nil || second.System.Uptime != "12d 3h" {
		t.Fatal("the last good reading was dropped rather than kept and labelled")
	}
	if second.UpdatedAt == nil || !second.UpdatedAt.Equal(*first.UpdatedAt) {
		t.Fatal("stale data was re-stamped with the time it was served, which is what makes old data look current")
	}
	if second.FailureClass != remote.ClassUnreachable {
		t.Fatalf("failure class = %q", second.FailureClass)
	}
}

func TestOverviewReportsUnavailableWithNothingCached(t *testing.T) {
	srv := overviewServer(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, &remote.Error{Class: remote.ClassAuthentication, Err: fmt.Errorf("ssh: unable to authenticate")}
	}, remotes("nas")...)

	got := getOverview(t, srv).Servers[0]
	if got.Status != "unavailable" {
		t.Fatalf("expected unavailable, got %q", got.Status)
	}
	if got.System != nil || got.UpdatedAt != nil {
		t.Fatal("a server that has never answered must not carry a reading or a timestamp")
	}
	if got.FailureClass != remote.ClassAuthentication {
		t.Fatalf("failure class = %q", got.FailureClass)
	}
}

// Remote errors name the address, the config file, ~/.ssh/known_hosts and
// whatever the far side printed. None of that may cross into a browser.
func TestOverviewNeverForwardsTheRemoteError(t *testing.T) {
	secret := "[nas] SSH connection failed (192.168.1.10:22): ssh: handshake failed\n  → Config: ~/.config/homebutler/config.yaml"
	srv := overviewServer(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		return nil, &remote.Error{Class: remote.ClassUnreachable, Err: fmt.Errorf("%s", secret)}
	}, remotes("nas")...)

	req := httptest.NewRequest("GET", "/api/overview", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	for _, leak := range []string{"192.168.1.10:22", "known_hosts", "config.yaml", "handshake failed"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the response carried %q", leak)
		}
	}
	if !strings.Contains(body, "The server did not answer.") {
		t.Fatalf("expected the safe sentence for the class, got %s", body)
	}
}

// One slow machine decided how long every other machine took. The deadline is
// what stops that, and the reading it was waiting for arrives on the next poll.
func TestOverviewDoesNotWaitForeverForOneServer(t *testing.T) {
	srv := overviewServer(func(s *config.ServerConfig, args ...string) ([]byte, error) {
		if s.Name == "slow" {
			time.Sleep(overviewDeadline + 2*time.Second)
			return []byte(`{"hostname":"slow"}`), nil
		}
		return []byte(`{"hostname":"quick","uptime":"1d"}`), nil
	}, remotes("quick", "slow")...)

	start := time.Now()
	got := getOverview(t, srv)
	elapsed := time.Since(start)

	if elapsed > overviewDeadline+time.Second {
		t.Fatalf("the overview waited %s for one slow server", elapsed)
	}
	byName := map[string]serverReading{}
	for _, r := range got.Servers {
		byName[r.Name] = r
	}
	if byName["quick"].Status != "current" {
		t.Fatalf("the quick server should not wait for the slow one, got %q", byName["quick"].Status)
	}
	if byName["slow"].Status != "unavailable" {
		t.Fatalf("a server that has not answered yet and has no snapshot is unavailable, got %q", byName["slow"].Status)
	}
}
