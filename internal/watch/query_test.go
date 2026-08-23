package watch

import (
	"testing"
	"time"
)

func seedIncidents(t *testing.T, dir string, specs ...Incident) {
	t.Helper()
	for i := range specs {
		inc := specs[i]
		if err := SaveIncident(dir, &inc, 0); err != nil {
			t.Fatalf("saving incident %q: %v", inc.ID, err)
		}
	}
}

// A limit has to keep the newest incidents. ListIncidents sorts newest first
// and History relies on that; if the sort ever flips, a limit would quietly
// return the oldest incidents instead, which is the opposite of what an agent
// asking "what broke" wants.
func TestHistoryKeepsTheNewestIncidents(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	seedIncidents(t, dir,
		Incident{ID: "oldest", Container: "nginx", DetectedAt: base},
		Incident{ID: "middle", Container: "nginx", DetectedAt: base.Add(time.Hour)},
		Incident{ID: "newest", Container: "nginx", DetectedAt: base.Add(2 * time.Hour)},
	)

	got, err := History(dir, HistoryOptions{Limit: 2})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("limit 2 returned %d incidents", len(got))
	}
	if got[0].ID != "newest" || got[1].ID != "middle" {
		t.Errorf("limit kept %q, %q; want newest, middle", got[0].ID, got[1].ID)
	}
}

// Logs are the reason this filter exists: every incident carries a hundred
// lines of container output twice over, and a caller that did not ask for them
// should not receive them.
func TestHistoryExcludesLogsUnlessAsked(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir, Incident{
		ID:         "i1",
		Container:  "nginx",
		DetectedAt: time.Now(),
		PreLogs:    "before the restart",
		PostLogs:   "after the restart",
	})

	got, err := History(dir, HistoryOptions{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(got))
	}
	if got[0].PreLogs != "" || got[0].PostLogs != "" {
		t.Error("logs must be excluded unless include_logs is set")
	}

	withLogs, err := History(dir, HistoryOptions{Logs: true})
	if err != nil {
		t.Fatalf("History with logs: %v", err)
	}
	if withLogs[0].PreLogs != "before the restart" || withLogs[0].PostLogs != "after the restart" {
		t.Error("logs must be present when asked for")
	}
}

func TestHistoryFiltersByContainer(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	seedIncidents(t, dir,
		Incident{ID: "a", Container: "nginx", DetectedAt: now},
		Incident{ID: "b", Container: "postgres", DetectedAt: now.Add(time.Minute)},
	)

	got, err := History(dir, HistoryOptions{Container: "NGINX"})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 1 || got[0].Container != "nginx" {
		t.Errorf("container filter returned %+v; want the single nginx incident", got)
	}
}

// A target that has never been polled is still being watched. Dropping it
// would make the list disagree with what watch start is actually monitoring.
func TestListWatchedIncludesTargetsWithNoStateYet(t *testing.T) {
	dir := t.TempDir()
	added := time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC)
	if err := SaveTargets(dir, []Target{
		{Container: "nginx", Kind: "docker", AddedAt: added},
		{Container: "cron.service", Kind: "systemd", AddedAt: added},
	}); err != nil {
		t.Fatalf("SaveTargets: %v", err)
	}
	if err := SaveState(dir, map[string]*ContainerState{
		"nginx": {Container: "nginx", RestartCount: 3, LastChecked: added.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := ListWatched(dir)
	if err != nil {
		t.Fatalf("ListWatched: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both targets, got %d", len(got))
	}

	byName := map[string]WatchedTarget{}
	for _, w := range got {
		byName[w.Container] = w
	}
	if byName["nginx"].RestartCount != 3 || byName["nginx"].LastChecked == "" {
		t.Errorf("nginx should carry its recorded state, got %+v", byName["nginx"])
	}
	if byName["cron.service"].Kind != "systemd" {
		t.Errorf("kind should survive the join, got %q", byName["cron.service"].Kind)
	}
	if byName["cron.service"].LastChecked != "" {
		t.Error("a target with no state should report no last-checked time rather than a zero one")
	}
}
