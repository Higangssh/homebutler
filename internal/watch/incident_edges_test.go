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
	if after.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644 as before", after.Mode().Perm())
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

// Incidents an earlier watch check recorded under the target's name are
// still on disk. Asked by the unit, they are found; and their container is
// the unit, which is the name report snapshots hold.
func TestAnIncidentRecordedUnderTheTargetsNameIsFoundByItsUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Kind: KindDocker, Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	old := Incident{ID: "blog-old", Container: "blog", Kind: KindDocker, DetectedAt: findBase}
	seedIncidents(t, dir, old, Incident{ID: "redis-1", Container: "redis", Kind: KindDocker, DetectedAt: findBase.Add(time.Hour)})

	for _, name := range []string{"paperless", "blog"} {
		got, err := find(t, dir, "", name)
		if err != nil || got.ID != "blog-old" {
			t.Errorf("FindIncident(%s) = %v, %v; want the incident recorded under the target's name", name, got, err)
		}
		hist, err := History(dir, HistoryOptions{Container: name})
		if err != nil || len(hist) != 1 || hist[0].ID != "blog-old" {
			t.Errorf("History(%s) = %+v, %v", name, hist, err)
		}
	}

	a := IncidentContainer(dir, old)
	if a.Container != "paperless" {
		t.Errorf("IncidentContainer = %+v; want the container's own name, paperless", a)
	}
	if !strings.Contains(a.Inferred, "blog") || !strings.Contains(a.Inferred, "paperless") {
		t.Errorf("Inferred = %q; want it to say the name was read from the watch list", a.Inferred)
	}

	kindless := IncidentContainer(dir, Incident{Container: "blog"})
	if kindless.Container != "paperless" || !strings.Contains(kindless.Inferred, "does not record its kind") {
		t.Errorf("kind-less IncidentContainer = %+v", kindless)
	}

	// One recorded under the unit is already the container's.
	if a := IncidentContainer(dir, Incident{Container: "paperless", Kind: KindDocker}); a.Container != "paperless" || a.Inferred != "" {
		t.Errorf("IncidentContainer(paperless) = %+v", a)
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
// incidents are not the container's.
func TestUnitAliasSkipsAnotherKindsIncidents(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "blog", Kind: KindDocker, Unit: "paperless"},
		{Container: "blog", Kind: KindSystemd},
	}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "blog-systemd", Container: "blog", Kind: KindSystemd, DetectedAt: findBase})
	if got, err := find(t, dir, "", "paperless"); err == nil {
		t.Errorf("paperless = %s; the systemd target's incident was taken for the container's", got.ID)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "paperless"}); len(hist) != 0 {
		t.Errorf("History(paperless) = %+v; the systemd target's incident was taken for the container's", hist)
	}
}
