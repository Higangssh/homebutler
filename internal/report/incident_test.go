package report

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/watch"
)

// incidentAt is the moment every test here places its incident: between the
// 03:00 and 04:00 snapshots.
var incidentAt = time.Date(2026, 11, 24, 3, 12, 0, 0, time.UTC)

func contextSnapshot(stamp string, containers []docker.Container, listeners []ports.PortInfo, diskUsed float64) *Snapshot {
	return &Snapshot{
		Timestamp:  stamp,
		ServerName: "pi",
		System:     &system.StatusInfo{Disks: []system.DiskInfo{{Mount: "/", TotalGB: 100, UsedGB: diskUsed}}},
		Containers: containers,
		Ports:      listeners,
		Processes:  []ProcessIdentity{{Name: "sshd", Hash: "0123456789ab"}},
	}
}

func saveAll(t *testing.T, dir string, snaps ...*Snapshot) {
	t.Helper()
	for _, s := range snaps {
		if err := saveSnapshot(dir, s); err != nil {
			t.Fatal(err)
		}
	}
}

func texts(lines []ChangeLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}

func noCurrent() (*Snapshot, error) {
	return nil, errors.New("the current state was read though a snapshot follows the incident")
}

// The question the tool exists for: of what changed around the crash, which
// changes were the crashed container's? A port is the container's by what
// inventory attributed it to, not by its name appearing in the line — the
// line for :8001 names docker-proxy, which is what holds the socket — and a
// container whose name merely contains the target's is somebody else.
func TestIncidentContextSplitsChangesByTheTargetsIdentity(t *testing.T) {
	dir := t.TempDir()
	paperless := docker.Container{ID: "aaa", Name: "paperless", Image: "paperless:2.1", State: "running"}
	db := docker.Container{ID: "bbb", Name: "paperless-db", Image: "postgres:16", State: "running"}
	redis := docker.Container{ID: "ccc", Name: "redis", Image: "redis:7", State: "running"}
	web := ports.PortInfo{Protocol: "tcp", Address: "0.0.0.0", Port: "8000", Process: "docker-proxy", Container: "paperless (paperless:2.1)"}
	cache := ports.PortInfo{Protocol: "tcp", Address: "0.0.0.0", Port: "6379", Process: "docker-proxy", Container: "redis (redis:7)"}

	upgraded := paperless
	upgraded.Image = "paperless:2.2"
	dbStopped := db
	dbStopped.State = "exited"
	webAfter := web
	webAfter.Container = "paperless (paperless:2.2)"
	api := ports.PortInfo{Protocol: "tcp", Address: "0.0.0.0", Port: "8001", Process: "docker-proxy", Container: "paperless (paperless:2.2)"}

	saveAll(t, dir,
		// Older than the snapshot before: what changed between it and the
		// next is not about this incident.
		contextSnapshot("2026-11-24T02:00:00Z", []docker.Container{paperless, db}, []ports.PortInfo{web}, 10),
		contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{paperless, db, redis}, []ports.PortInfo{web, cache}, 10),
		contextSnapshot("2026-11-24T04:00:00Z", []docker.Container{upgraded, dbStopped}, []ports.PortInfo{webAfter, api}, 14),
		// Newer than the snapshot after: the same.
		contextSnapshot("2026-11-24T05:00:00Z", []docker.Container{upgraded, dbStopped, {ID: "ddd", Name: "late", Image: "late:1", State: "running"}}, []ports.PortInfo{webAfter, api}, 14),
	)

	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: true})
	if err != nil {
		t.Fatal(err)
	}

	if !got.Compared || got.AfterIsCurrent {
		t.Fatalf("compared = %v, after_is_current = %v; want a comparison between two snapshots", got.Compared, got.AfterIsCurrent)
	}
	if got.Before != "2026-11-24T03:00:00Z" || got.After != "2026-11-24T04:00:00Z" {
		t.Errorf("compared %s → %s, want the snapshots either side of 03:12", got.Before, got.After)
	}
	if got.Incident.ID != "paperless-1" {
		t.Errorf("incident = %q, want the one asked about", got.Incident.ID)
	}

	wantAbout := []string{
		"new: :8001/tcp — now docker-proxy",
		"image: paperless — paperless:2.1 → paperless:2.2",
	}
	wantOther := []string{
		"gone: :6379/tcp — was docker-proxy",
		"gone: redis — was running",
		"state: paperless-db — running → exited",
		"disk: / — +4.0 GB since last report",
	}
	assertSameLines(t, "about_target", texts(got.AboutTarget), wantAbout)
	assertSameLines(t, "other_changes", texts(got.OtherChanges), wantOther)
}

func assertSameLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	g := map[string]bool{}
	for _, l := range got {
		g[l] = true
	}
	for _, l := range want {
		if !g[l] {
			t.Errorf("%s is missing %q; got %q", what, l, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s = %q, want exactly %q", what, got, want)
	}
}

// Nothing before the incident is nothing to compare with, and the answer has
// to say so rather than hand back two empty lists that read as a quiet night.
func TestIncidentContextWithNoSnapshotBeforeSaysSo(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir, contextSnapshot("2026-11-24T04:00:00Z", nil, nil, 10))

	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Compared {
		t.Error("compared = true with no snapshot before the incident")
	}
	if got.Incident.ID != "paperless-1" {
		t.Error("the incident has to come back on its own")
	}
	if !strings.Contains(got.Note, "No snapshot was taken before this incident") {
		t.Errorf("note = %q, want it to say no snapshot precedes the incident", got.Note)
	}
	if len(got.AboutTarget)+len(got.OtherChanges) != 0 {
		t.Errorf("changes listed with nothing compared: %v %v", got.AboutTarget, got.OtherChanges)
	}

	// No snapshot directory at all is the same answer, not an error.
	got, err = Context(inc, ContextOptions{SnapshotDir: dir + "/missing", Current: noCurrent, Container: true})
	if err != nil || got.Compared {
		t.Errorf("missing snapshot directory: compared = %v, err = %v", got != nil && got.Compared, err)
	}
}

// With no report since the incident, the comparison runs to the current
// state, and says that is what it is: a change since then can be the fix.
func TestIncidentContextWithNoSnapshotAfterComparesTheCurrentState(t *testing.T) {
	dir := t.TempDir()
	paperless := docker.Container{ID: "aaa", Name: "paperless", Image: "paperless:2.1", State: "running"}
	saveAll(t, dir, contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{paperless}, nil, 10))

	recreated := paperless
	recreated.ID = "fff"
	read := false
	current := func() (*Snapshot, error) {
		read = true
		return contextSnapshot("2026-11-24T09:00:00Z", []docker.Container{recreated}, nil, 10), nil
	}

	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: current, Container: true})
	if err != nil {
		t.Fatal(err)
	}
	if !read || !got.Compared || !got.AfterIsCurrent {
		t.Fatalf("read = %v, compared = %v, after_is_current = %v; want the current state compared", read, got.Compared, got.AfterIsCurrent)
	}
	if got.After != "2026-11-24T09:00:00Z" {
		t.Errorf("after = %q, want when the current state was read", got.After)
	}
	if !strings.Contains(got.Note, "current state") || !strings.Contains(got.Note, "not to the moment") {
		t.Errorf("note = %q, want it to say this is the current state and not the moment of the crash", got.Note)
	}
	assertSameLines(t, "about_target", texts(got.AboutTarget), []string{"replaced: paperless — recreated, aaa → fff"})

	// Where the current state cannot be read, nothing is compared, and the
	// snapshot that was found is still named.
	got, err = Context(inc, ContextOptions{SnapshotDir: dir, Container: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Compared || got.Before != "2026-11-24T03:00:00Z" || got.Note == "" {
		t.Errorf("without a current state: compared = %v, before = %q, note = %q", got.Compared, got.Before, got.Note)
	}
}

// A systemd unit or a pm2 app is not a container, so no change can be tied
// to it by identity, even one that shares its name.
func TestIncidentContextForANonContainerTargetTiesNothingToIt(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir,
		contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{{ID: "a", Name: "caddy", Image: "caddy:2", State: "running"}}, nil, 10),
		contextSnapshot("2026-11-24T04:00:00Z", nil, nil, 10),
	)
	inc := watch.Incident{ID: "caddy-1", Container: "caddy", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AboutTarget) != 0 {
		t.Errorf("about_target = %q for a target that is not a container", texts(got.AboutTarget))
	}
	assertSameLines(t, "other_changes", texts(got.OtherChanges), []string{"gone: caddy — was running"})
}

// A collector that did not answer qualifies both lists. Left only in the
// other list, an empty about_target would say nothing happened to the
// container when nobody looked. The process collector is not one of these:
// a container's own processes are never compared, so its silence says
// nothing about the container.
func TestIncidentContextPutsASkippedComparisonInBothLists(t *testing.T) {
	dir := t.TempDir()
	before := contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{{ID: "a", Name: "paperless", State: "running"}}, nil, 10)
	after := contextSnapshot("2026-11-24T04:00:00Z", nil, nil, 10)
	after.Failed = []string{inventory.CollectorDocker, inventory.CollectorProcesses}
	saveAll(t, dir, before, after)

	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: true})
	if err != nil {
		t.Fatal(err)
	}
	skipped := "skipped: containers — not compared — Docker did not answer"
	processes := "skipped: processes — not compared — the process collector did not answer"
	assertSameLines(t, "about_target", texts(got.AboutTarget), []string{skipped})
	assertSameLines(t, "other_changes", texts(got.OtherChanges), []string{skipped, processes})
}
