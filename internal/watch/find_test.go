package watch

import (
	"strings"
	"testing"
	"time"
)

// A target means its latest incident, not the first one found on disk, and
// the match is the one watch history uses.
func TestFindIncidentByTargetIsTheLatest(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	seedIncidents(t, dir,
		Incident{ID: "paperless-old", Container: "paperless", DetectedAt: base, PreLogs: "old"},
		Incident{ID: "paperless-new", Container: "paperless", DetectedAt: base.Add(time.Hour), PreLogs: "new"},
		Incident{ID: "redis-newest", Container: "redis", DetectedAt: base.Add(2 * time.Hour)},
	)

	got, err := FindIncident(dir, "", "Paperless")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "paperless-new" {
		t.Errorf("latest for paperless = %q, want paperless-new", got.ID)
	}
	if got.PreLogs != "new" {
		t.Error("the incident comes back whole; leaving logs out is the caller's choice")
	}

	got, err = FindIncident(dir, "paperless-old", "")
	if err != nil || got.ID != "paperless-old" {
		t.Errorf("by ID: %v, %v", got, err)
	}

	for _, tc := range []struct{ id, target, want string }{
		{"", "", "required"},
		{"paperless-old", "paperless", "not both"},
		{"", "nextcloud", `no incident recorded for "nextcloud"`},
	} {
		if _, err := FindIncident(dir, tc.id, tc.target); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("FindIncident(%q, %q) err = %v, want %q", tc.id, tc.target, err, tc.want)
		}
	}
}

// Only a container can have report changes tied to it, so the kind has to
// come from the watch list, where systemd and pm2 targets say what they are.
func TestIsContainerIncidentReadsTheKindFromTheWatchList(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "paperless"},
		{Container: "caddy", Kind: KindSystemd, Unit: "caddy.service"},
		{Container: "api", Kind: KindPM2},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		inc  Incident
		want bool
	}{
		{Incident{Container: "paperless"}, true},
		{Incident{Container: "caddy.service"}, false},
		{Incident{Container: "api"}, false},
		{Incident{Container: "pve/100", Source: "proxmox"}, false},
		// Removed from the list since: docker is the kind a target has
		// when none is given.
		{Incident{Container: "gone"}, true},
	} {
		if got := IsContainerIncident(dir, tc.inc); got != tc.want {
			t.Errorf("IsContainerIncident(%q, source %q) = %v, want %v", tc.inc.Container, tc.inc.Source, got, tc.want)
		}
	}
}
