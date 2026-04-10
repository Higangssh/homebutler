package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTargetsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{
		{Container: "nginx", AddedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Container: "redis", AddedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
	}

	if err := SaveTargets(dir, targets); err != nil {
		t.Fatalf("SaveTargets: %v", err)
	}

	loaded, err := LoadTargets(dir)
	if err != nil {
		t.Fatalf("LoadTargets: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(loaded))
	}
	if loaded[0].Container != "nginx" {
		t.Errorf("expected nginx, got %s", loaded[0].Container)
	}
	if loaded[1].Container != "redis" {
		t.Errorf("expected redis, got %s", loaded[1].Container)
	}
}

func TestLoadTargets_NotExist(t *testing.T) {
	dir := t.TempDir()
	targets, err := LoadTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets != nil {
		t.Errorf("expected nil, got %v", targets)
	}
}

func TestLoadTargets_Corrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "targets.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTargets(dir)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
}

func TestLoadTargets_InvalidKind(t *testing.T) {
	dir := t.TempDir()
	data := `[{"container":"app","kind":"kubernetes","added_at":"2025-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, "targets.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	targets, err := LoadTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	// Invalid kind should be defaulted to "docker"
	if targets[0].Kind != "docker" {
		t.Errorf("expected kind=docker after invalid kind correction, got %s", targets[0].Kind)
	}
}

func TestLoadTargets_AllValidKinds(t *testing.T) {
	dir := t.TempDir()
	data := `[
		{"container":"a","kind":"docker"},
		{"container":"b","kind":"systemd"},
		{"container":"c","kind":"pm2"},
		{"container":"d","kind":""}
	]`
	if err := os.WriteFile(filepath.Join(dir, "targets.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	targets, err := LoadTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 4 {
		t.Fatalf("expected 4 targets, got %d", len(targets))
	}
	// All valid kinds should be preserved
	if targets[0].Kind != "docker" {
		t.Errorf("expected docker, got %s", targets[0].Kind)
	}
	if targets[1].Kind != "systemd" {
		t.Errorf("expected systemd, got %s", targets[1].Kind)
	}
	if targets[2].Kind != "pm2" {
		t.Errorf("expected pm2, got %s", targets[2].Kind)
	}
	if targets[3].Kind != "" {
		t.Errorf("expected empty kind, got %s", targets[3].Kind)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	states := map[string]*ContainerState{
		"nginx": {
			Container:    "nginx",
			RestartCount: 3,
			StartedAt:    "2025-01-01T00:00:00Z",
			LastChecked:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	if err := SaveState(dir, states); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	s, ok := loaded["nginx"]
	if !ok {
		t.Fatal("nginx state not found")
	}
	if s.RestartCount != 3 {
		t.Errorf("expected RestartCount=3, got %d", s.RestartCount)
	}
	if s.StartedAt != "2025-01-01T00:00:00Z" {
		t.Errorf("unexpected StartedAt: %s", s.StartedAt)
	}
}

func TestLoadState_NotExist(t *testing.T) {
	dir := t.TempDir()
	states, err := LoadState(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected empty map, got %v", states)
	}
}

func TestLoadState_Corrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadState(dir)
	if err == nil {
		t.Fatal("expected error for corrupt state.json")
	}
	if !strings.Contains(err.Error(), "corrupt state.json") {
		t.Errorf("expected 'corrupt state.json' in error, got: %v", err)
	}
}

func TestIncidentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	inc := &Incident{
		ID:           GenerateIncidentID("nginx", now),
		Container:    "nginx",
		DetectedAt:   now,
		RestartCount: 5,
		PrevStarted:  "2025-06-15T09:00:00Z",
		CurrStarted:  "2025-06-15T10:29:00Z",
		PreLogs:      "pre-restart log lines",
		PostLogs:     "post-restart log lines",
	}

	if err := SaveIncident(dir, inc); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}

	loaded, err := LoadIncident(dir, inc.ID)
	if err != nil {
		t.Fatalf("LoadIncident: %v", err)
	}
	if loaded.Container != "nginx" {
		t.Errorf("expected nginx, got %s", loaded.Container)
	}
	if loaded.RestartCount != 5 {
		t.Errorf("expected RestartCount=5, got %d", loaded.RestartCount)
	}
	if loaded.PostLogs != "post-restart log lines" {
		t.Errorf("unexpected PostLogs: %s", loaded.PostLogs)
	}
}

func TestLoadIncident_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadIncident(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing incident")
	}
}

func TestLoadIncident_InvalidID(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadIncident(dir, "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for invalid incident ID")
	}
}

func TestLoadIncident_InvalidIDChars(t *testing.T) {
	dir := t.TempDir()
	invalidIDs := []string{"id with space", "id\ttab", "id;semicolon", "id&amp"}
	for _, id := range invalidIDs {
		_, err := LoadIncident(dir, id)
		if err == nil {
			t.Errorf("expected error for invalid ID %q", id)
		}
		if err != nil && !strings.Contains(err.Error(), "invalid incident ID") {
			t.Errorf("expected 'invalid incident ID' for %q, got: %v", id, err)
		}
	}
}

func TestLoadIncident_ValidIDChars(t *testing.T) {
	dir := t.TempDir()
	// Valid chars: a-z, A-Z, 0-9, -, _, .
	validID := "nginx-20250615-103045.000-abc123"
	_, err := LoadIncident(dir, validID)
	// Should fail with "not found", NOT "invalid incident ID"
	if err == nil {
		t.Fatal("expected error for missing incident")
	}
	if strings.Contains(err.Error(), "invalid incident ID") {
		t.Errorf("valid ID was rejected: %s", validID)
	}
}

func TestListIncidents_Empty(t *testing.T) {
	dir := t.TempDir()
	incidents, err := ListIncidents(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if incidents != nil {
		t.Errorf("expected nil, got %v", incidents)
	}
}

func TestListIncidents_Sorted(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	for _, pair := range []struct {
		name string
		t    time.Time
	}{
		{"redis", t1},
		{"nginx", t3},
		{"postgres", t2},
	} {
		inc := &Incident{
			ID:         GenerateIncidentID(pair.name, pair.t),
			Container:  pair.name,
			DetectedAt: pair.t,
		}
		if err := SaveIncident(dir, inc); err != nil {
			t.Fatal(err)
		}
	}

	incidents, err := ListIncidents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incidents) != 3 {
		t.Fatalf("expected 3 incidents, got %d", len(incidents))
	}
	if incidents[0].Container != "nginx" {
		t.Errorf("expected nginx first (most recent), got %s", incidents[0].Container)
	}
	if incidents[2].Container != "redis" {
		t.Errorf("expected redis last (oldest), got %s", incidents[2].Container)
	}
}

func TestListIncidents_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()
	idir := filepath.Join(dir, "incidents")
	if err := os.MkdirAll(idir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a non-JSON file
	if err := os.WriteFile(filepath.Join(idir, "readme.txt"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a directory inside incidents/
	if err := os.MkdirAll(filepath.Join(idir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a valid incident
	inc := &Incident{
		ID:         GenerateIncidentID("nginx", time.Now()),
		Container:  "nginx",
		DetectedAt: time.Now(),
	}
	if err := SaveIncident(dir, inc); err != nil {
		t.Fatal(err)
	}

	incidents, err := ListIncidents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incidents) != 1 {
		t.Errorf("expected 1 incident (skipping non-json), got %d", len(incidents))
	}
}

func TestListIncidents_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	idir := filepath.Join(dir, "incidents")
	if err := os.MkdirAll(idir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a corrupt JSON file
	if err := os.WriteFile(filepath.Join(idir, "corrupt.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a valid incident
	inc := &Incident{
		ID:         GenerateIncidentID("nginx", time.Now()),
		Container:  "nginx",
		DetectedAt: time.Now(),
	}
	if err := SaveIncident(dir, inc); err != nil {
		t.Fatal(err)
	}

	incidents, err := ListIncidents(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt JSON should be skipped
	if len(incidents) != 1 {
		t.Errorf("expected 1 incident (skipping corrupt json), got %d", len(incidents))
	}
}

func TestGenerateIncidentID(t *testing.T) {
	ts := time.Date(2025, 6, 15, 10, 30, 45, 0, time.UTC)
	id := GenerateIncidentID("nginx", ts)
	// Format: nginx-20250615-103045.000-<6hex>
	if len(id) < len("nginx-20250615-103045.000-") {
		t.Errorf("ID too short: %s", id)
	}
	prefix := "nginx-20250615-103045.000-"
	if id[:len(prefix)] != prefix {
		t.Errorf("unexpected ID prefix: %s (expected prefix %s)", id, prefix)
	}
	// Two calls should produce different IDs (random suffix)
	id2 := GenerateIncidentID("nginx", ts)
	if id == id2 {
		t.Errorf("expected different IDs for same timestamp, got %s both times", id)
	}
}

func TestGenerateIncidentID_Uniqueness(t *testing.T) {
	ts := time.Date(2025, 6, 15, 10, 30, 45, 0, time.UTC)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateIncidentID("app", ts)
		if seen[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestGenerateIncidentID_DifferentContainers(t *testing.T) {
	ts := time.Date(2025, 6, 15, 10, 30, 45, 0, time.UTC)
	id1 := GenerateIncidentID("nginx", ts)
	id2 := GenerateIncidentID("redis", ts)
	if strings.HasPrefix(id1, "redis") {
		t.Error("nginx ID should not start with redis")
	}
	if strings.HasPrefix(id2, "nginx") {
		t.Error("redis ID should not start with nginx")
	}
	if id1 == id2 {
		t.Error("different containers should have different IDs")
	}
}

func TestTarget_EffectiveKind(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		expected string
	}{
		{"empty kind defaults to docker", "", "docker"},
		{"docker kind", "docker", "docker"},
		{"systemd kind", "systemd", "systemd"},
		{"pm2 kind", "pm2", "pm2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := Target{Container: "x", Kind: tc.kind}
			got := target.EffectiveKind()
			if got != tc.expected {
				t.Errorf("EffectiveKind()=%s, expected %s", got, tc.expected)
			}
		})
	}
}

func TestTarget_EffectiveUnit(t *testing.T) {
	tests := []struct {
		name      string
		container string
		unit      string
		expected  string
	}{
		{"empty unit defaults to container", "nginx", "", "nginx"},
		{"unit set", "alias", "real-container", "real-container"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := Target{Container: tc.container, Unit: tc.unit}
			got := target.EffectiveUnit()
			if got != tc.expected {
				t.Errorf("EffectiveUnit()=%s, expected %s", got, tc.expected)
			}
		})
	}
}

func TestWatchDir(t *testing.T) {
	dir, err := WatchDir()
	if err != nil {
		t.Fatalf("WatchDir: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".homebutler", "watch")) {
		t.Errorf("unexpected WatchDir: %s", dir)
	}
}

func TestSaveTargets_CreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "subdir", "deep")
	targets := []Target{{Container: "x"}}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatalf("SaveTargets should create dir: %v", err)
	}
	loaded, err := LoadTargets(dir)
	if err != nil {
		t.Fatalf("LoadTargets: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 target, got %d", len(loaded))
	}
}

func TestSaveState_CreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "subdir", "deep")
	states := map[string]*ContainerState{"x": {Container: "x"}}
	if err := SaveState(dir, states); err != nil {
		t.Fatalf("SaveState should create dir: %v", err)
	}
}

func TestSaveIncident_CreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "subdir", "deep")
	inc := &Incident{
		ID:        GenerateIncidentID("x", time.Now()),
		Container: "x",
	}
	if err := SaveIncident(dir, inc); err != nil {
		t.Fatalf("SaveIncident should create dir: %v", err)
	}
}
