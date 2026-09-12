package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/watch"
)

// withWatchDir points watch.WatchDir at a temporary home, so these tests read a
// directory they wrote rather than whatever this machine happens to have.
func withWatchDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := watch.WatchDir()
	if err != nil {
		t.Fatalf("watch dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "incidents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

func watchServer() *Server {
	cfg := &config.Config{
		Servers: []config.ServerConfig{{Name: "myserver", Host: "192.168.1.10", Local: true}},
		Alerts:  config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
	cfg.Watch.Retention.MaxIncidents = 50
	return New(cfg, "127.0.0.1", 8080)
}

func writeIncident(t *testing.T, dir, name string, inc watch.Incident) {
	t.Helper()
	data, err := json.Marshal(inc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "incidents", name), data, 0o644); err != nil {
		t.Fatalf("write incident: %v", err)
	}
}

func getJSON(t *testing.T, srv *Server, path string, into any) int {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if into != nil && w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("decode %s: %v (body %s)", path, err, w.Body.String())
		}
	}
	return w.Code
}

// A watch directory that was never used is an empty view, not a failure. It is
// also the state every machine is in before `watch add` is run once.
func TestWatchOverview_NothingWatchedYet(t *testing.T) {
	withWatchDir(t)

	var got watchOverview
	if code := getJSON(t, watchServer(), "/api/watch", &got); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Targets) != 0 {
		t.Fatalf("expected no targets, got %d", len(got.Targets))
	}
	if got.Retention.Kept != 0 {
		t.Fatalf("expected no incidents kept, got %d", got.Retention.Kept)
	}
	if got.Retention.Max != 50 {
		t.Fatalf("expected the configured limit of 50, got %d", got.Retention.Max)
	}
}

func TestWatchOverview_ReportsTargetsAndRetention(t *testing.T) {
	dir := withWatchDir(t)
	if err := watch.SaveTargets(dir, []watch.Target{{Container: "plex", Kind: "docker", AddedAt: time.Now()}}); err != nil {
		t.Fatalf("save targets: %v", err)
	}
	writeIncident(t, dir, "plex-20260410-224033.581-7a2124.json", watch.Incident{
		ID: "plex-20260410-224033.581-7a2124", Container: "plex", DetectedAt: time.Now(),
	})

	var got watchOverview
	if code := getJSON(t, watchServer(), "/api/watch", &got); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Targets) != 1 || got.Targets[0].Container != "plex" {
		t.Fatalf("expected plex on the watch list, got %+v", got.Targets)
	}
	if got.Retention.Kept != 1 {
		t.Fatalf("expected 1 incident kept, got %d", got.Retention.Kept)
	}
}

// The count above the list has to agree with the list. ListIncidentRefs cannot
// parse an incident written before the filename gained its millisecond and
// suffix fields, while the history that fills the list reads it anyway.
func TestWatchOverview_CountsIncidentsTheListWillShow(t *testing.T) {
	dir := withWatchDir(t)
	writeIncident(t, dir, "plex-20260410-224033.581-7a2124.json", watch.Incident{
		ID: "plex-20260410-224033.581-7a2124", Container: "plex", DetectedAt: time.Now(),
	})
	writeIncident(t, dir, "gitea-20260410-174933.json", watch.Incident{
		ID: "gitea-20260410-174933", Container: "gitea", DetectedAt: time.Now().Add(-time.Hour),
	})

	srv := watchServer()

	var overview watchOverview
	getJSON(t, srv, "/api/watch", &overview)

	var incidents []watch.Incident
	getJSON(t, srv, "/api/watch/incidents", &incidents)

	if overview.Retention.Kept != len(incidents) {
		t.Fatalf("overview says %d kept, the list has %d", overview.Retention.Kept, len(incidents))
	}
}

// The list is fetched on every page load and every incident carries two
// hundred lines of captured output, so it must not include them.
func TestWatchIncidents_ListCarriesNoLogs(t *testing.T) {
	dir := withWatchDir(t)
	writeIncident(t, dir, "plex-20260410-224033.581-7a2124.json", watch.Incident{
		ID:        "plex-20260410-224033.581-7a2124",
		Container: "plex",
		PreLogs:   "buffer grew to 2.1 GB",
		PostLogs:  "Starting Plex Media Server",
	})

	var incidents []watch.Incident
	if code := getJSON(t, watchServer(), "/api/watch/incidents", &incidents); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	if incidents[0].PreLogs != "" || incidents[0].PostLogs != "" {
		t.Fatal("the list carried logs; opening one incident is what fetches them")
	}
}

// Opening one incident is the only reason the logs were captured before the
// container died rather than read afterwards.
func TestWatchIncident_CarriesTheCapturedLogs(t *testing.T) {
	dir := withWatchDir(t)
	writeIncident(t, dir, "plex-20260410-224033.581-7a2124.json", watch.Incident{
		ID:        "plex-20260410-224033.581-7a2124",
		Container: "plex",
		PreLogs:   "buffer grew to 2.1 GB",
		PostLogs:  "Starting Plex Media Server",
	})

	var incident watch.Incident
	code := getJSON(t, watchServer(), "/api/watch/incidents/plex-20260410-224033.581-7a2124", &incident)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if incident.PreLogs != "buffer grew to 2.1 GB" {
		t.Fatalf("expected the pre-restart logs, got %q", incident.PreLogs)
	}
}

func TestWatchIncident_UnknownID(t *testing.T) {
	withWatchDir(t)
	if code := getJSON(t, watchServer(), "/api/watch/incidents/nope", nil); code != http.StatusNotFound {
		t.Fatalf("expected 404 for an incident that does not exist, got %d", code)
	}
}
