package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A kind-less incident under the unit is taken for the container's only
// while no target of another kind has the unit's name: that target's
// incidents are recorded under it too, and IncidentContainer refuses the
// same guess.
func TestUnitAliasSkipsKindlessIncidentsWhenAnotherKindHasTheUnitsName(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "front", Kind: KindDocker, Unit: "web"},
		{Container: "web", Kind: KindSystemd},
	}); err != nil {
		t.Fatal(err)
	}
	kindless := Incident{ID: "web-kindless", Container: "web", DetectedAt: findBase}
	seedIncidents(t, dir, kindless)
	if hist, _ := History(dir, HistoryOptions{Container: "front"}); len(hist) != 0 {
		t.Errorf("History(front) = %+v; a kind-less incident a systemd target may own was taken for the container's", hist)
	}
	if got, err := find(t, dir, "", "front"); err == nil {
		t.Errorf("FindIncident(front) = %s; a kind-less incident a systemd target may own was taken for the container's", got.ID)
	}
	if a := IncidentContainer(dir, kindless); a.Container != "" {
		t.Errorf("IncidentContainer = %+v; want it refused, as the alias now is", a)
	}

	// Without the systemd target, the same incident is the container's.
	if err := SaveTargets(dir, []Target{{Container: "front", Kind: KindDocker, Unit: "web"}}); err != nil {
		t.Fatal(err)
	}
	if hist, _ := History(dir, HistoryOptions{Container: "front"}); len(hist) != 1 {
		t.Errorf("History(front) = %+v; want the kind-less incident under the unit", hist)
	}
	if got, err := find(t, dir, "", "front"); err != nil || got.ID != "web-kindless" {
		t.Errorf("FindIncident(front) = %v, %v", got, err)
	}
}

// A Proxmox incident records no kind, and is not a container's whatever name
// it carries.
func TestUnitAliasSkipsProxmoxIncidents(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "pve", Kind: KindDocker, Unit: "proxmox-home"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "proxmox-home-1", Container: "proxmox-home", Source: "proxmox", DetectedAt: findBase})
	if hist, _ := History(dir, HistoryOptions{Container: "pve"}); len(hist) != 0 {
		t.Errorf("History(pve) = %+v; a Proxmox incident was taken for the container's", hist)
	}
	if got, err := find(t, dir, "", "pve"); err == nil {
		t.Errorf("FindIncident(pve) = %s; a Proxmox incident was taken for the container's", got.ID)
	}
}

// History has always matched a name ignoring case, and the target that name
// picks out is matched the same way, so FRONT finds what front finds.
func TestHistoryFoldsTheTargetItAliases(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "front", Kind: KindDocker, Unit: "web"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "web-1", Container: "web", Kind: KindDocker, DetectedAt: findBase})
	if hist, _ := History(dir, HistoryOptions{Container: "FRONT"}); len(hist) != 1 {
		t.Errorf("History(FRONT) = %+v; want what History(front) gives", hist)
	}
}

// A watch list that cannot be read leaves the alias out, and says so
// without the path.
func TestHistoryWarnsWhenTheWatchListCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir, Incident{ID: "web-1", Container: "web", Kind: KindDocker, DetectedAt: findBase})
	if err := os.WriteFile(targetsPath(dir), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	hist, warnings, err := HistoryWithWarnings(dir, HistoryOptions{Container: "web"})
	if err != nil || len(hist) != 1 {
		t.Fatalf("HistoryWithWarnings = %+v, %v; the name alone still filters", hist, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "watch list could not be read") || strings.Contains(warnings[0], dir) {
		t.Errorf("warnings = %q; want the watch list warning, without the path", warnings)
	}
	if _, warnings, _ := HistoryWithWarnings(dir, HistoryOptions{}); len(warnings) != 0 {
		t.Errorf("unfiltered warnings = %q; the list is not read without a name", warnings)
	}
}

// A reloading unit's main process is running, so a start in the second the
// last run exited is the new run's, as it is for an active one.
func TestSystemdReloadingUnitKeepsCurrStart(t *testing.T) {
	inc := watchSystemdOnce(t,
		"ActiveState=active\nSubState=running\nExecMainStartTimestamp=@1790772404",
		"ActiveState=reloading\nSubState=reload\nExecMainStartTimestamp=@1790772420\nExecMainExitTimestamp=@1790772420",
	)
	if inc.CurrStarted != "2026-09-30T12:47:00Z" {
		t.Errorf("CurrStarted = %q; a reloading unit's start was dropped", inc.CurrStarted)
	}
}

// The sweep takes only names SaveIncident writes, so a file an operator put
// beside the incidents survives however it is named.
func TestIsTempNameIsExactlyWhatSaveIncidentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-20261124-030000.000-abcdef.json")
	for range 20 {
		if name := filepath.Base(tempName(path)); !isTempName(name) {
			t.Fatalf("isTempName(%q) = false for a name SaveIncident writes", name)
		}
	}
	for _, name := range []string{
		".notes.json.bak.tmp",
		".web.json.0A1B2C3D4E5F.tmp",
		".web.json.0a1b2c.tmp",
		".web.json.0a1b2c3d4e5f0.tmp",
		".web.json.zz1b2c3d4e5f.tmp",
		".json.0a1b2c3d4e5f.tmp",
		"..json.0a1b2c3d4e5f.tmp",
		".a.tmp",
		"web.json.0a1b2c3d4e5f.tmp",
		".web.json.0a1b2c3d4e5f",
		".web.jsonx0a1b2c3d4e5f.tmp",
	} {
		if isTempName(name) {
			t.Errorf("isTempName(%q) = true; the sweep would remove a file SaveIncident did not write", name)
		}
	}
}

// The sweep leaves an old file that only looks like a temp file.
func TestPruneLeavesFilesSaveIncidentDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir, Incident{ID: GenerateIncidentID("web", findBase), Container: "web", DetectedAt: findBase})
	idir := incidentsDir(dir)
	var paths []string
	for _, name := range []string{".notes.json.bak.tmp", ".web.json.0a1b2c.tmp", ".web.json.0A1B2C3D4E5F.tmp"} {
		p := filepath.Join(idir, name)
		if err := os.WriteFile(p, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := findBase
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if _, err := PruneIncidents(dir, 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(p), err)
		}
	}
}
