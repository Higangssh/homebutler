package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/watch"
)

// `watch check` stamps detected_at when it runs. Run after a report that
// already saw the restarted container, detected_at alone would make that
// report the "before" side and find nothing. The new run's start time is
// what the check read, and the window is placed there.
func TestIncidentContextAnchorsOnTheRestartNotTheCheck(t *testing.T) {
	dir := t.TempDir()
	web := docker.Container{ID: "aaa", Name: "web", Image: "web:1", State: "running"}
	upgraded := web
	upgraded.ID, upgraded.Image = "bbb", "web:2"
	saveAll(t, dir,
		contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{web}, nil, 10),
		// Taken after the restart and before the check.
		contextSnapshot("2026-11-24T03:20:00Z", []docker.Container{upgraded}, nil, 10),
	)
	inc := watch.Incident{
		ID: "web-1", Container: "web", Kind: watch.KindDocker,
		DetectedAt:  time.Date(2026, 11, 24, 3, 25, 0, 0, time.UTC),
		CurrStarted: "2026-11-24T03:10:05.5Z",
	}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if got.AnchoredOn != "curr_started_at" || got.AnchoredAt != "2026-11-24T03:10:05.5Z" {
		t.Errorf("anchored on %s at %s, want curr_started_at at 03:10:05.5", got.AnchoredOn, got.AnchoredAt)
	}
	if got.Before != "2026-11-24T03:00:00Z" || got.After != "2026-11-24T03:20:00Z" || !got.Compared {
		t.Fatalf("compared %v %s → %s, want the reports either side of the restart", got.Compared, got.Before, got.After)
	}
	assertSameLines(t, "about_target", texts(got.AboutTarget), []string{"replaced: web — recreated, aaa → bbb, web:1 → web:2"})

	// A start time later than detected_at leaves detected_at.
	inc.CurrStarted = "2026-11-24T03:30:00Z"
	if got, err := Context(inc, ContextOptions{SnapshotDir: dir, Container: "web"}); err != nil || got.AnchoredOn != "detected_at" {
		t.Errorf("anchored on %s with a start after detection", got.AnchoredOn)
	}
}

// Two saved snapshots bracket the incident; they do not order the changes
// inside the bracket. The answer says so whenever it compared anything.
func TestIncidentContextAlwaysSaysTheWindowRunsPastTheIncident(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir,
		contextSnapshot("2026-11-24T03:00:00Z", nil, nil, 10),
		contextSnapshot("2026-11-24T04:00:00Z", nil, nil, 20),
	)
	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "paperless"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Compared || got.AfterIsCurrent || !strings.Contains(got.Note, "may have happened after the incident") {
		t.Errorf("compared = %v, note = %q; want the note to say a change may have come after", got.Compared, got.Note)
	}
}

// A snapshot's stamp is to the second. One stamped in the incident's own
// second may have been taken after it, so it is the after side, down to an
// exact tie.
func TestIncidentContextTreatsASameSecondSnapshotAsAfter(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir,
		contextSnapshot("2026-11-24T03:00:00Z", nil, nil, 10),
		contextSnapshot("2026-11-24T03:12:00Z", nil, nil, 10),
	)
	for _, at := range []time.Time{incidentAt, incidentAt.Add(400 * time.Millisecond)} {
		inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: at}
		got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "paperless"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Before != "2026-11-24T03:00:00Z" || got.After != "2026-11-24T03:12:00Z" {
			t.Errorf("incident at %s: %s → %s, want the 03:12:00 snapshot as after", at.Format(time.RFC3339Nano), got.Before, got.After)
		}
	}
}

// With snapshots kept but none older than the incident, the reason is
// pruning or a late first report, and the note says so instead of telling
// someone who runs report to start running it. Inside a container report is
// not run at all, so it is not suggested there.
func TestIncidentContextNotesSayWhyNothingWasCompared(t *testing.T) {
	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}

	dir := t.TempDir()
	saveAll(t, dir, contextSnapshot("2026-11-24T05:00:00Z", nil, nil, 10))
	got, _ := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "paperless"})
	if !strings.Contains(got.Note, "pruned") || !strings.Contains(got.Note, "--keep") || strings.Contains(got.Note, "on a schedule") {
		t.Errorf("only newer snapshots kept: note = %q", got.Note)
	}

	empty := t.TempDir()
	got, _ = Context(inc, ContextOptions{SnapshotDir: empty, Current: noCurrent, Container: "paperless"})
	if !strings.Contains(got.Note, "on a schedule") {
		t.Errorf("no snapshots: note = %q, want the suggestion", got.Note)
	}
	got, _ = Context(inc, ContextOptions{SnapshotDir: empty, InContainer: true, Container: "paperless"})
	if strings.Contains(got.Note, "Run report") {
		t.Errorf("in a container: note = %q, suggests running report", got.Note)
	}
}

// An unreadable snapshot is named by its time, with the reason and without
// the path: the answer may be relayed anywhere, and the path is the
// operator's home directory.
func TestIncidentContextUnreadableSnapshotsKeepPathsOut(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions")
	}
	inc := watch.Incident{ID: "paperless-1", Container: "paperless", DetectedAt: incidentAt}
	for _, side := range []string{"before", "after"} {
		dir := t.TempDir()
		saveAll(t, dir,
			contextSnapshot("2026-11-24T03:00:00Z", nil, nil, 10),
			contextSnapshot("2026-11-24T04:00:00Z", nil, nil, 10),
		)
		name := "snapshot_20261124T030000Z.json"
		if side == "after" {
			name = "snapshot_20261124T040000Z.json"
		}
		if err := os.Chmod(filepath.Join(dir, name), 0o000); err != nil {
			t.Fatal(err)
		}
		got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "paperless"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Compared || !strings.Contains(got.Note, "could not be read") {
			t.Errorf("%s unreadable: compared = %v, note = %q", side, got.Compared, got.Note)
		}
		if strings.Contains(got.Note, dir) || strings.Contains(got.Note, "snapshot_") {
			t.Errorf("%s unreadable: the note carries a path: %q", side, got.Note)
		}
		if got.Before != "2026-11-24T03:00:00Z" || got.After != "2026-11-24T04:00:00Z" {
			t.Errorf("%s unreadable: before = %q, after = %q; want both named", side, got.Before, got.After)
		}
	}
}

// A port the container published only on the before side, gone after it, is
// still the container's: the attribution is read from both snapshots. A
// ports collector that did not answer qualifies the container's lines too.
func TestIncidentContextPortsFromEitherSideAndSkippedPorts(t *testing.T) {
	web := docker.Container{ID: "aaa", Name: "web", Image: "web:1", State: "running"}
	published := ports.PortInfo{Protocol: "tcp", Address: "0.0.0.0", Port: "18414", Container: "web (web:1)"}
	inc := watch.Incident{ID: "web-1", Container: "web", DetectedAt: incidentAt}

	dir := t.TempDir()
	saveAll(t, dir,
		contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{web}, []ports.PortInfo{published}, 10),
		contextSnapshot("2026-11-24T04:00:00Z", []docker.Container{web}, nil, 10),
	)
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "web"})
	if err != nil {
		t.Fatal(err)
	}
	assertSameLines(t, "about_target", texts(got.AboutTarget), []string{"gone: :18414/tcp — was web (web:1)"})

	dir = t.TempDir()
	after := contextSnapshot("2026-11-24T04:00:00Z", []docker.Container{web}, nil, 10)
	after.Failed = []string{inventory.CollectorPorts}
	saveAll(t, dir, contextSnapshot("2026-11-24T03:00:00Z", []docker.Container{web}, []ports.PortInfo{published}, 10), after)
	got, err = Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "web"})
	if err != nil {
		t.Fatal(err)
	}
	skipped := "skipped: ports — not compared — the port collector did not answer"
	assertSameLines(t, "about_target", texts(got.AboutTarget), []string{skipped})
	assertSameLines(t, "other_changes", texts(got.OtherChanges), []string{skipped})
}

// The current state is read by CurrentState, which collects the way Run does
// and saves nothing: ports are attributed to the container that published
// them, so the comparison ties them to it, and the snapshot directory is
// left as it was.
func TestIncidentContextReadsTheCurrentStateThroughTheCollectors(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir, contextSnapshot("2026-11-24T03:00:00Z", nil, nil, 60))

	web := docker.Container{ID: "aaa", Name: "web", Image: "web:1", State: "running", Ports: "0.0.0.0:8080->80/tcp"}
	fns := fakeFuncs([]docker.Container{web}, []ports.PortInfo{{Protocol: "tcp", Address: "0.0.0.0", Port: "8080", Process: "docker-proxy"}})
	fns.ProcessesFn = func() ([]system.ProcessInfo, error) {
		return []system.ProcessInfo{{Name: "sshd", Command: "sshd", Elapsed: time.Hour}}, nil
	}
	inc := watch.Incident{ID: "web-1", Container: "web", DetectedAt: incidentAt}
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: CurrentState(nil, fns), Container: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Compared || !got.AfterIsCurrent {
		t.Fatalf("compared = %v, after_is_current = %v", got.Compared, got.AfterIsCurrent)
	}
	assertSameLines(t, "about_target", texts(got.AboutTarget), []string{
		"new: web — now running",
		"new: :8080/tcp — now docker-proxy",
	})
	files, _ := listSnapshotFiles(dir)
	if len(files) != 1 {
		t.Errorf("reading the current state left %d snapshots, want the one there was", len(files))
	}
}

// curr_started_at is not a new run when it is the start of the run that
// died, which the systemd monitor wrote for a unit that failed without
// starting again, and not when it is Docker's zero time for a container
// that never started. Either would place the window at the wrong moment:
// the first weeks before the failure.
func TestIncidentContextIgnoresAStartThatIsNotANewRun(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir,
		contextSnapshot("2026-11-05T03:00:00Z", nil, nil, 10),
		contextSnapshot("2026-11-24T03:00:00Z", nil, nil, 10),
	)
	for _, tc := range []struct{ prev, curr string }{
		{"2026-11-05T04:00:00Z", "2026-11-05T04:00:00Z"},
		{"2026-11-05T04:00:00Z", "0001-01-01T00:00:00Z"},
	} {
		inc := watch.Incident{ID: "u-1", Container: "u", Kind: watch.KindSystemd, DetectedAt: incidentAt, PrevStarted: tc.prev, CurrStarted: tc.curr}
		got, err := Context(inc, ContextOptions{SnapshotDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		if got.AnchoredOn != "detected_at" || got.Before != "2026-11-24T03:00:00Z" {
			t.Errorf("curr %s, prev %s: anchored on %s, before %s; want detected_at and the snapshot before it", tc.curr, tc.prev, got.AnchoredOn, got.Before)
		}
	}
}

// A snapshot directory that cannot be listed is an error, and the error does
// not carry the directory, which is the operator's home.
func TestIncidentContextListingErrorKeepsThePathOut(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "snapshots")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	inc := watch.Incident{ID: "web-1", Container: "web", DetectedAt: incidentAt}
	_, err := Context(inc, ContextOptions{SnapshotDir: notADir, Container: "web"})
	if err == nil {
		t.Fatal("listing a file as a directory did not fail")
	}
	if strings.Contains(err.Error(), notADir) {
		t.Errorf("err = %q carries the path", err)
	}
}

// An attribution read from the watch list as it is now is said to be one,
// on top of whatever the note says about the comparison.
func TestIncidentContextSaysWhenTheContainerWasInferred(t *testing.T) {
	dir := t.TempDir()
	saveAll(t, dir,
		contextSnapshot("2026-11-24T03:00:00Z", nil, nil, 10),
		contextSnapshot("2026-11-24T04:00:00Z", nil, nil, 10),
	)
	inc := watch.Incident{ID: "web-1", Container: "web", DetectedAt: incidentAt}
	inferred := "This incident does not record its kind; it is taken to be the container's because the watch list now has a docker target of that name."
	got, err := Context(inc, ContextOptions{SnapshotDir: dir, Current: noCurrent, Container: "web", Inferred: inferred})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Note, "may have happened after") || !strings.HasSuffix(got.Note, inferred) {
		t.Errorf("note = %q, want the comparison and then the inference", got.Note)
	}
}
