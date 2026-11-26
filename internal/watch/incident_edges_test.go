package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// watchSystemdOnce runs the systemd monitor from seed to the first incident.
func watchSystemdOnce(t *testing.T, seed, poll string) Incident {
	t.Helper()
	polls := 0
	runner := func(name string, args ...string) (string, error) {
		switch name {
		case "systemctl":
			if !strings.Contains(strings.Join(args, " "), "ExecMainExitTimestamp") {
				return "", fmt.Errorf("ExecMainExitTimestamp was not asked for: %v", args)
			}
			polls++
			if polls == 1 {
				return seed, nil
			}
			return poll, nil
		case "journalctl":
			return "journal", nil
		}
		return "", fmt.Errorf("unexpected command: %s", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	incCh := make(chan Incident, 1)
	sm := &SystemdMonitor{Run: runner, Interval: 20 * time.Millisecond}
	go func() { _ = sm.Watch(ctx, []Target{{Container: "api", Kind: KindSystemd}}, incCh) }()
	select {
	case inc := <-incCh:
		return inc
	case <-ctx.Done():
		t.Fatal("timed out waiting for incident")
	}
	return Incident{}
}

// A unit that restarts and fails again between two polls reports a new start,
// but that is the start of the run that died. Written as curr_started_at it
// would place incident_context's window at a moment before the crash.
func TestSystemdRestartThenFailWithinOnePollHasNoCurrStart(t *testing.T) {
	inc := watchSystemdOnce(t,
		"ActiveState=active\nSubState=running\nExecMainStartTimestamp=@1790772404\nExecMainExitTimestamp=n/a",
		"ActiveState=failed\nSubState=failed\nExecMainStartTimestamp=@1790772420\nExecMainExitTimestamp=@1790772425\nExecMainStatus=1",
	)
	if inc.CurrStarted != "" {
		t.Errorf("CurrStarted = %q; the run that started then died is not a run after the crash", inc.CurrStarted)
	}
	if inc.PrevStarted != "2026-09-30T12:46:44Z" {
		t.Errorf("PrevStarted = %q", inc.PrevStarted)
	}
}

// systemd's timestamps are to the second, so a running unit that restarted
// in the second its last run exited shows an exit equal to its start. It is
// running, and its start is the new run's.
func TestSystemdRestartInTheSecondOfTheExitKeepsCurrStart(t *testing.T) {
	inc := watchSystemdOnce(t,
		"ActiveState=active\nSubState=running\nExecMainStartTimestamp=@1790772404",
		"ActiveState=active\nSubState=running\nExecMainStartTimestamp=@1790772420\nExecMainExitTimestamp=@1790772420",
	)
	if inc.CurrStarted != "2026-09-30T12:47:00Z" {
		t.Errorf("CurrStarted = %q; a running unit's start was dropped", inc.CurrStarted)
	}
}

// An incident is renamed into place, never rewritten where it lies: a reader
// that opens it mid-write must see the old file or the new one. A file
// rewritten in place keeps its identity, so this fails if SaveIncident goes
// back to writing the final name directly.
func TestSaveIncidentReplacesTheFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	inc := Incident{ID: "web-1", Container: "web", DetectedAt: findBase}
	if err := SaveIncident(dir, &inc, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(incidentsDir(dir), "web-1.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	inc.PreLogs = "enriched"
	if err := SaveIncident(dir, &inc, 0); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the incident was rewritten in place, so a concurrent reader can see half of it")
	}
	entries, err := os.ReadDir(incidentsDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("incident directory holds %d entries, want only the incident", len(entries))
	}
	got, err := LoadIncident(dir, "web-1")
	if err != nil || got.PreLogs != "enriched" {
		t.Errorf("LoadIncident = %+v, %v", got, err)
	}
}

// A temp file left by a crash mid-write is not an incident, so a lookup does
// not warn that it could not be read, and prune leaves it alone.
func TestLookupIgnoresALeftoverTempFile(t *testing.T) {
	dir := t.TempDir()
	id := GenerateIncidentID("web", findBase)
	seedIncidents(t, dir, Incident{ID: id, Container: "web", DetectedAt: findBase})
	f, err := os.CreateTemp(incidentsDir(dir), ".web-2.json.*"+tempSuffix)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"id": "web-2", "contai`)
	_ = f.Close()

	inc, warnings, err := FindIncident(dir, "", "web")
	if err != nil || inc.ID != id {
		t.Fatalf("FindIncident = %v, %v", inc, err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q; a half-written temp file was read as an incident", warnings)
	}
	if n, err := CountIncidents(dir); err != nil || n != 1 {
		t.Errorf("CountIncidents = %d, %v; want the one incident", n, err)
	}
}

// watch check inspects a docker target watched under a unit by the unit,
// which is the container's real name, and records under it, as the daemon
// does. It used to inspect and record under the target's name.
func TestCheckTargetsUsesADockerTargetsUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Kind: KindDocker, Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dir, map[string]*ContainerState{
		"blog": {Container: "paperless", RestartCount: 1, StartedAt: "2026-11-24T02:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	origInspect, origCapture := inspectContainerFunc, captureLogsFunc
	defer func() { inspectContainerFunc, captureLogsFunc = origInspect, origCapture }()
	var inspected, logged []string
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		inspected = append(inspected, name)
		return &InspectResult{RestartCount: 2, StartedAt: "2026-11-24T03:00:00Z", Running: true}, nil
	}
	captureLogsFunc = func(name, _ string) string {
		logged = append(logged, name)
		return "logs"
	}

	res, err := CheckTargets(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(inspected, ",") != "paperless" || strings.Join(logged, ",") != "paperless" {
		t.Errorf("inspected %v and read logs of %v; want the unit, paperless", inspected, logged)
	}
	if len(res.Incidents) != 1 || res.Incidents[0].Container != "paperless" || !strings.HasPrefix(res.Incidents[0].ID, "paperless-") {
		t.Fatalf("incidents = %+v; want one recorded under paperless", res.Incidents)
	}
	states, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := states["blog"]; s == nil || s.Container != "paperless" || s.RestartCount != 2 {
		t.Errorf("state = %+v; want it kept under the target, naming the container", states)
	}
}

// watch add seeds the baseline from the container check will inspect.
func TestAddTargetSeedsStateFromTheUnit(t *testing.T) {
	dir := t.TempDir()
	origInspect := inspectContainerFunc
	defer func() { inspectContainerFunc = origInspect }()
	var inspected []string
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		inspected = append(inspected, name)
		return &InspectResult{RestartCount: 3, StartedAt: "2026-11-24T03:00:00Z", Running: true}, nil
	}
	if _, err := AddTarget(dir, Target{Container: "blog", Kind: KindDocker, Unit: "paperless"}); err != nil {
		t.Fatal(err)
	}
	states, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(inspected, ",") != "paperless" {
		t.Errorf("inspected %v; want the unit, paperless", inspected)
	}
	if s := states["blog"]; s == nil || s.Container != "paperless" || s.RestartCount != 3 {
		t.Errorf("state = %+v; want it under the target, naming the container", states)
	}
}

// State an earlier check recorded by inspecting the target's name came from
// another container, so it is not a baseline for the unit.
func TestCheckTargetsDoesNotCompareAgainstAnotherContainersState(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Kind: KindDocker, Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dir, map[string]*ContainerState{
		"blog": {Container: "blog", RestartCount: 0, StartedAt: "2026-11-01T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	origInspect, origCapture := inspectContainerFunc, captureLogsFunc
	defer func() { inspectContainerFunc, captureLogsFunc = origInspect, origCapture }()
	inspectContainerFunc = func(string) (*InspectResult, error) {
		return &InspectResult{RestartCount: 4, StartedAt: "2026-11-24T03:00:00Z", Running: true}, nil
	}
	captureLogsFunc = func(string, string) string { return "" }

	res, err := CheckTargets(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Incidents) != 0 {
		t.Errorf("incidents = %+v; another container's state was taken as this one's baseline", res.Incidents)
	}
}

// An earlier watch check inspected a --unit target by its listed name, so an
// incident it recorded under that name is about the container literally
// called that, never the unit. Asked by the unit, it is not found, and it is
// attributed to the container whose name it carries.
func TestAnIncidentRecordedUnderTheListedNameIsNotTheUnits(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Kind: KindDocker, Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	old := Incident{ID: "blog-old", Container: "blog", Kind: KindDocker, DetectedAt: findBase}
	seedIncidents(t, dir, old)

	if got, err := find(t, dir, "", "paperless"); err == nil {
		t.Errorf("FindIncident(paperless) = %s; an incident of the container called blog was taken for paperless's", got.ID)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "paperless"}); len(hist) != 0 {
		t.Errorf("History(paperless) = %+v; an incident of the container called blog was taken for paperless's", hist)
	}
	if got, err := find(t, dir, "", "blog"); err != nil || got.ID != "blog-old" {
		t.Errorf("FindIncident(blog) = %v, %v; want it found by its own name", got, err)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "blog"}); len(hist) != 1 {
		t.Errorf("History(blog) = %+v; want it found by its own name", hist)
	}
	if a := IncidentContainer(dir, old); a.Container != "blog" || a.Inferred != "" {
		t.Errorf("IncidentContainer = %+v; want the container the name it carries, blog", a)
	}
	if a := IncidentContainer(dir, Incident{Container: "blog"}); a.Container != "blog" {
		t.Errorf("kind-less IncidentContainer = %+v; want blog", a)
	}
}

// The daemon records a --unit target's incidents under the unit, and the
// target's listed name finds them there, in watch show and watch history.
func TestTheListedNameFindsIncidentsRecordedUnderTheUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Kind: KindDocker, Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "paperless-1", Container: "paperless", Kind: KindDocker, DetectedAt: findBase})
	if got, err := find(t, dir, "", "blog"); err != nil || got.ID != "paperless-1" {
		t.Errorf("FindIncident(blog) = %v, %v", got, err)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "blog"}); len(hist) != 1 || hist[0].ID != "paperless-1" {
		t.Errorf("History(blog) = %+v; want the incident recorded under its unit", hist)
	}
}

// One target's unit can be another target's name. The alias is one step from
// a target to its own unit, never a chain through the next target.
func TestUnitAliasDoesNotChainAcrossTargets(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "front", Kind: KindDocker, Unit: "web2"},
		{Container: "web2", Kind: KindDocker, Unit: "other"},
	}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "web2-1", Container: "web2", Kind: KindDocker, DetectedAt: findBase})
	if got, err := find(t, dir, "", "other"); err == nil {
		t.Errorf("FindIncident(other) = %s; web2's incident was taken for other's", got.ID)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "other"}); len(hist) != 0 {
		t.Errorf("History(other) = %+v; web2's incident was taken for other's", hist)
	}
	if got, err := find(t, dir, "", "front"); err != nil || got.ID != "web2-1" {
		t.Errorf("FindIncident(front) = %v, %v; want the incident under its unit, web2", got, err)
	}
}

// A systemd target's unit is never an alias, in either direction.
func TestUnitAliasIsOnlyADockerTargets(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "web", Kind: KindSystemd, Unit: "nginx"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "web-1", Container: "web", Kind: KindSystemd, DetectedAt: findBase})
	if got, err := find(t, dir, "", "nginx"); err == nil {
		t.Errorf("nginx = %s; the systemd target's incident was taken for its unit's", got.ID)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "nginx"}); len(hist) != 0 {
		t.Errorf("History(nginx) = %+v", hist)
	}
}

// The list keys on name and kind, so a systemd target can share a docker
// target's name. Asked by the docker target's unit, the systemd target's
// incidents, kind-less ones included, are not the container's; and asked by
// the shared name, an incident of another kind under the unit is not either.
func TestUnitAliasSkipsAnotherKindsIncidents(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "web", Kind: KindDocker, Unit: "real"},
		{Container: "web", Kind: KindSystemd},
	}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir,
		Incident{ID: "web-kindless", Container: "web", DetectedAt: findBase},
		Incident{ID: "web-systemd", Container: "web", Kind: KindSystemd, DetectedAt: findBase.Add(time.Minute)},
	)
	if got, err := find(t, dir, "", "real"); err == nil {
		t.Errorf("FindIncident(real) = %s; the systemd target's incident was taken for the container's", got.ID)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "real"}); len(hist) != 0 {
		t.Errorf("History(real) = %+v; the systemd target's incident was taken for the container's", hist)
	}

	dir = t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Kind: KindDocker, Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "paperless-pm2", Container: "paperless", Kind: KindPM2, DetectedAt: findBase})
	if hist, _ := History(dir, HistoryOptions{Container: "blog"}); len(hist) != 0 {
		t.Errorf("History(blog) = %+v; a pm2 incident under the unit was taken for the container's", hist)
	}
}

// Targets sharing a unit are one container, so one restart of it is one
// incident, not one per target; three would trip the default flapping
// threshold on their own.
func TestCheckTargetsRecordsOneIncidentPerUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "a", Kind: KindDocker, Unit: "x"},
		{Container: "x", Kind: KindDocker},
		{Container: "b", Kind: KindDocker, Unit: "x"},
	}); err != nil {
		t.Fatal(err)
	}
	prev := func() *ContainerState {
		return &ContainerState{Container: "x", RestartCount: 1, StartedAt: "2026-11-24T02:00:00Z"}
	}
	if err := SaveState(dir, map[string]*ContainerState{"a": prev(), "x": prev(), "b": prev()}); err != nil {
		t.Fatal(err)
	}
	origInspect, origCapture := inspectContainerFunc, captureLogsFunc
	defer func() { inspectContainerFunc, captureLogsFunc = origInspect, origCapture }()
	calls := 0
	inspectContainerFunc = func(string) (*InspectResult, error) {
		calls++
		return &InspectResult{RestartCount: 2, StartedAt: "2026-11-24T03:00:00Z", Running: true}, nil
	}
	captureLogsFunc = func(string, string) string { return "" }

	res, err := CheckTargets(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Incidents) != 1 {
		t.Errorf("incidents = %d; want one for the one restart of x", len(res.Incidents))
	}
	if calls != 1 {
		t.Errorf("x was inspected %d times, want once", calls)
	}
	if n, _ := CountIncidents(dir); n != 1 {
		t.Errorf("%d incidents on disk, want 1", n)
	}
	states, _ := LoadState(dir)
	for _, name := range []string{"a", "x", "b"} {
		if s := states[name]; s == nil || s.RestartCount != 2 {
			t.Errorf("state[%s] = %+v; every target sharing the unit moves to the new baseline", name, s)
		}
	}
	if res.Checked != 3 {
		t.Errorf("Checked = %d, want the three targets", res.Checked)
	}
}

// A Restart=on-failure unit that dies inside a poll is not failed: it is
// activating towards its next attempt, or inactive once the attempts run out.
// Its start is the dead run's either way.
func TestSystemdRunThatDiedWhileNotFailedHasNoCurrStart(t *testing.T) {
	for _, state := range []string{"ActiveState=activating\nSubState=auto-restart", "ActiveState=inactive\nSubState=dead"} {
		inc := watchSystemdOnce(t,
			"ActiveState=active\nSubState=running\nExecMainStartTimestamp=@1790772404",
			state+"\nExecMainStartTimestamp=@1790772420\nExecMainExitTimestamp=@1790772425",
		)
		if inc.CurrStarted != "" {
			t.Errorf("%s: CurrStarted = %q; the run that died is not a run after the crash", state, inc.CurrStarted)
		}
	}
}

// Prune sweeps temp files a crashed write left once they are an hour old,
// leaves a younger one, which may be a write in progress, and does not count
// either as an incident.
func TestPruneSweepsOldTempFiles(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir, Incident{ID: GenerateIncidentID("web", findBase), Container: "web", DetectedAt: findBase})
	idir := incidentsDir(dir)
	stale := filepath.Join(idir, ".web-1.json.0a1b2c3d4e5f"+tempSuffix)
	fresh := filepath.Join(idir, ".web-2.json.0f1e2d3c4b5a"+tempSuffix)
	other := filepath.Join(idir, "notes.json.old.tmp")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, p := range []string{stale, other} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	n, err := PruneIncidents(dir, 0)
	if err != nil || n != 0 {
		t.Errorf("PruneIncidents = %d, %v; want nothing counted", n, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a temp file two hours old was left: %v", err)
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(p), err)
		}
	}
	if c, _ := CountIncidents(dir); c != 1 {
		t.Errorf("CountIncidents = %d, want the one incident", c)
	}
}

// A replacement that cannot be renamed into place leaves what was there and
// says so, and leaves no temp file behind. On Windows that is a rename while
// another process holds the file open; here the destination is a directory.
func TestSaveIncidentThatCannotRenameLeavesTheOldFile(t *testing.T) {
	dir := t.TempDir()
	idir := incidentsDir(dir)
	blocker := filepath.Join(idir, "web-1.json")
	if err := os.MkdirAll(filepath.Join(blocker, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	inc := Incident{ID: "web-1", Container: "web", DetectedAt: findBase}
	if err := SaveIncident(dir, &inc, 0); err == nil {
		t.Fatal("SaveIncident returned nil although the rename could not happen")
	}
	if _, err := os.Stat(filepath.Join(blocker, "keep")); err != nil {
		t.Errorf("what was at the destination is gone: %v", err)
	}
	entries, _ := os.ReadDir(idir)
	for _, e := range entries {
		if isTempName(e.Name()) {
			t.Errorf("temp file %s was left behind", e.Name())
		}
	}
}
