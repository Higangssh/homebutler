package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A single log line has no bound of its own — docker logs --tail and
// journalctl -n count lines, not bytes — so the cap has to be applied where
// every writer passes. #81.
func TestSaveIncidentBoundsCapturedLogs(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("x", MaxLogBytes*3)

	inc := Incident{
		ID:         GenerateIncidentID("svc", time.Now()),
		Container:  "svc",
		DetectedAt: time.Now(),
		PreLogs:    huge,
		PostLogs:   huge,
	}
	if err := SaveIncident(dir, &inc, 0); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}

	loaded, err := LoadIncident(dir, inc.ID)
	if err != nil {
		t.Fatalf("LoadIncident: %v", err)
	}
	for name, got := range map[string]string{"pre": loaded.PreLogs, "post": loaded.PostLogs} {
		if len(got) > MaxLogBytes+64 {
			t.Errorf("%s logs kept %d bytes, cap is %d", name, len(got), MaxLogBytes)
		}
		if !strings.HasPrefix(got, "… truncated ") {
			t.Errorf("%s logs were cut without saying so: %.40q", name, got)
		}
	}
}

// Truncation keeps the end. The last thing a process said is what explains why
// it stopped; the beginning is what can be spared.
func TestTruncateLogKeepsTheTail(t *testing.T) {
	s := strings.Repeat("old\n", MaxLogBytes) + "PANIC: the last line\n"
	got := TruncateLog(s)

	if !strings.HasSuffix(got, "PANIC: the last line\n") {
		t.Error("truncation dropped the tail, which is the half that explains the crash")
	}
	if len(got) > MaxLogBytes+64 {
		t.Errorf("kept %d bytes, cap is %d plus the marker", len(got), MaxLogBytes)
	}
	// The first surviving line is a whole line, not the back half of one.
	first, _, _ := strings.Cut(strings.TrimPrefix(got, "… truncated "), "\n")
	if !strings.HasSuffix(first, "bytes …") {
		t.Errorf("expected the marker on its own line, got %q", first)
	}
}

// A log shorter than the cap is returned untouched — no marker, no copy.
func TestTruncateLogLeavesShortLogsAlone(t *testing.T) {
	s := "two\nlines\n"
	if got := TruncateLog(s); got != s {
		t.Errorf("TruncateLog rewrote a log that fits: %q", got)
	}
}

// Pruning and flapping read the container and the time from the filename, so
// neither opens an incident. This is what stops one save from costing two full
// reads of the history.
func TestListIncidentRefsReadsNoFileBodies(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		inc := Incident{ID: GenerateIncidentID("my-api", at), Container: "my-api", DetectedAt: at}
		if err := SaveIncident(dir, &inc, 0); err != nil {
			t.Fatal(err)
		}
	}

	// Make every body unreadable. Refs must still come back complete.
	idir := filepath.Join(dir, "incidents")
	entries, err := os.ReadDir(idir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.WriteFile(filepath.Join(idir, e.Name()), []byte("{ not json"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	refs, err := ListIncidentRefs(dir)
	if err != nil {
		t.Fatalf("ListIncidentRefs: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs from filenames alone, got %d", len(refs))
	}
	if refs[0].Container != "my-api" {
		t.Errorf("container not recovered from the filename: %q", refs[0].Container)
	}
	if !refs[0].DetectedAt.After(refs[2].DetectedAt) {
		t.Error("refs are not sorted newest first")
	}
}

// A name that is not ours is not deleted. The criterion moved from "cannot be
// unmarshalled" to "cannot be named"; the policy did not.
func TestPruneLeavesUnrecognisedFilenames(t *testing.T) {
	dir := t.TempDir()
	idir := filepath.Join(dir, "incidents")
	if err := os.MkdirAll(idir, 0o755); err != nil {
		t.Fatal(err)
	}
	stranger := filepath.Join(idir, "notes.json")
	if err := os.WriteFile(stranger, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		inc := Incident{ID: GenerateIncidentID("svc", at), Container: "svc", DetectedAt: at}
		if err := SaveIncident(dir, &inc, 0); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := PruneIncidents(dir, 1); err != nil {
		t.Fatalf("PruneIncidents: %v", err)
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("pruning deleted a file it could not name: %v", err)
	}
}
