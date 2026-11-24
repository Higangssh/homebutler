package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var findBase = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)

func find(t *testing.T, dir, id, target string) (*Incident, error) {
	t.Helper()
	inc, _, err := FindIncident(dir, id, target)
	return inc, err
}

// A target means its latest incident, not the first one found on disk.
func TestFindIncidentByTargetIsTheLatest(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir,
		Incident{ID: "paperless-old", Container: "paperless", DetectedAt: findBase, PreLogs: "old"},
		Incident{ID: "paperless-new", Container: "paperless", DetectedAt: findBase.Add(time.Hour), PreLogs: "new"},
		Incident{ID: "redis-newest", Container: "redis", DetectedAt: findBase.Add(2 * time.Hour)},
	)

	got, err := find(t, dir, "", "paperless")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "paperless-new" {
		t.Errorf("latest for paperless = %q, want paperless-new", got.ID)
	}
	if got.PreLogs != "new" {
		t.Error("the incident comes back whole; leaving logs out is the caller's choice")
	}

	got, err = find(t, dir, "paperless-old", "")
	if err != nil || got.ID != "paperless-old" {
		t.Errorf("by ID: %v, %v", got, err)
	}

	for _, tc := range []struct{ id, target, want string }{
		{"", "", "required"},
		{"paperless-old", "paperless", "not both"},
		{"", "nextcloud", `no incident recorded for "nextcloud"`},
		{"", "-x", "invalid target"},
		{"-x", "", "invalid incident ID"},
	} {
		if _, err := find(t, dir, tc.id, tc.target); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("FindIncident(%q, %q) err = %v, want %q", tc.id, tc.target, err, tc.want)
		}
	}
}

// Docker names are case-sensitive: Web and web are two containers. The exact
// name wins; a name that differs only in case is used when it is the only
// one, and refused when there are two.
func TestFindIncidentMatchesTheNameExactly(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir,
		Incident{ID: "web-lower", Container: "web", DetectedAt: findBase},
		Incident{ID: "web-upper", Container: "Web", DetectedAt: findBase.Add(time.Hour)},
		Incident{ID: "api-only", Container: "API", DetectedAt: findBase},
	)
	got, err := find(t, dir, "", "web")
	if err != nil || got.ID != "web-lower" {
		t.Errorf("web = %v, %v; want web's own incident although Web's is newer", got, err)
	}
	got, err = find(t, dir, "", "api")
	if err != nil || got.ID != "api-only" {
		t.Errorf("api = %v, %v; want the one name that differs only in case", got, err)
	}
	if _, err := find(t, dir, "", "WEB"); err == nil || !strings.Contains(err.Error(), "differ from it only in case") {
		t.Errorf("WEB with web and Web recorded: err = %v, want it refused as ambiguous", err)
	}
}

// watch list shows a target by its own name, and a docker target added with
// a unit is watched, and recorded, under the unit. A name that differs only
// in case goes through the same resolution.
func TestFindIncidentResolvesADockerTargetWatchedUnderAUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "paperless-1", Container: "paperless", DetectedAt: findBase})
	for _, name := range []string{"blog", "BLOG"} {
		got, err := find(t, dir, "", name)
		if err != nil || got.ID != "paperless-1" {
			t.Errorf("%s = %v, %v; want the incident recorded under its unit", name, got, err)
		}
	}
}

// Systemd and pm2 incidents are recorded under the target's own name, so a
// systemd target's unit is not an alias: a docker container that happens to
// carry the unit's name is somebody else.
func TestFindIncidentDoesNotAliasASystemdUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "web", Kind: KindSystemd, Unit: "nginx"},
		{Container: "nginx"},
	}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "nginx-docker", Container: "nginx", Kind: KindDocker, DetectedAt: findBase})
	if got, err := find(t, dir, "", "web"); err == nil {
		t.Errorf("web = %s; the systemd target's unit was taken for the docker container", got.ID)
	}
}

// An incident file that cannot be read may be the newer incident. It is a
// warning, naming the file and not the directory, rather than a silent skip,
// and when it leaves nothing it is the error.
func TestFindIncidentWarnsAboutUnreadableFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions")
	}
	dir := t.TempDir()
	seedIncidents(t, dir,
		Incident{ID: "web-1", Container: "web", DetectedAt: findBase},
		Incident{ID: "web-2", Container: "web", DetectedAt: findBase.Add(time.Hour)},
	)
	if err := os.Chmod(filepath.Join(incidentsDir(dir), "web-2.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := FindIncident(dir, "", "web")
	if err != nil || got.ID != "web-1" {
		t.Fatalf("web = %v, %v", got, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "web-2.json") || strings.Contains(warnings[0], dir) {
		t.Errorf("warnings = %q, want one naming web-2.json without the directory", warnings)
	}

	if err := os.Chmod(filepath.Join(incidentsDir(dir), "web-1.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, _, err = FindIncident(dir, "", "web")
	if err == nil || !strings.Contains(err.Error(), "could not be read") || strings.Contains(err.Error(), dir) {
		t.Errorf("nothing readable: err = %v, want the unreadable files named without the directory", err)
	}
	_, _, err = FindIncident(dir, "web-1", "")
	if err == nil || strings.Contains(err.Error(), dir) {
		t.Errorf("by ID: err = %v, want it without the directory", err)
	}
}

// Only a container can have report changes tied to it, and only when that
// is known: the incident's own kind, or one watched target of one kind,
// which is said to be inferred from the list as it is now.
func TestIncidentContainerOnlyAnswersWhenTheKindIsKnown(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "paperless"},
		{Container: "caddy", Kind: KindSystemd, Unit: "caddy.service"},
		{Container: "api", Kind: KindPM2},
		// The list keys on name and kind, so both can be there.
		{Container: "web"},
		{Container: "web", Kind: KindSystemd},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		inc      Incident
		want     string
		whyHint  string
		inferred bool
	}{
		{Incident{Container: "paperless"}, "paperless", "", true},
		{Incident{Container: "caddy"}, "", "taken not to be a container", false},
		{Incident{Container: "api"}, "", "now has a pm2 target", false},
		{Incident{Container: "pve/100", Source: "proxmox"}, "", "proxmox", false},
		{Incident{Container: "gone"}, "", "no watched target has its name", false},
		{Incident{Container: "web"}, "", "more than one kind", false},
		// A recorded kind settles it, whatever the list says.
		{Incident{Container: "web", Kind: KindDocker}, "web", "", false},
		{Incident{Container: "web", Kind: KindSystemd}, "", "recorded by the systemd monitor", false},
		{Incident{Container: "gone", Kind: KindDocker}, "gone", "", false},
	} {
		a := IncidentContainer(dir, tc.inc)
		if a.Container != tc.want || !strings.Contains(a.Why, tc.whyHint) || (a.Container == "") == (a.Why == "") || (a.Inferred != "") != tc.inferred {
			t.Errorf("IncidentContainer(%q, kind %q, source %q) = %+v; want %q, a reason mentioning %q, inferred %v",
				tc.inc.Container, tc.inc.Kind, tc.inc.Source, a, tc.want, tc.whyHint, tc.inferred)
		}
	}
}

// A docker target's unit is an alias for docker incidents only. A systemd
// target can be named what the unit is, and its incidents, recorded under
// that name, are not the docker target's.
func TestFindIncidentAliasSkipsIncidentsOfAnotherKind(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{
		{Container: "web", Kind: KindDocker, Unit: "web-1"},
		{Container: "web-1", Kind: KindSystemd, Unit: "web-1.service"},
	}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir,
		Incident{ID: "web-1-docker", Container: "web-1", Kind: KindDocker, DetectedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Incident{ID: "web-1-systemd", Container: "web-1", Kind: KindSystemd, DetectedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)},
	)
	if got, err := find(t, dir, "", "web"); err != nil || got.ID != "web-1-docker" {
		t.Errorf("web = %v, %v; want the docker incident recorded under its unit", got, err)
	}
	if got, err := find(t, dir, "", "web-1"); err != nil || got.ID != "web-1-systemd" {
		t.Errorf("web-1 = %v, %v; want its own latest incident", got, err)
	}
}

// Docker names are case-sensitive, so a watched target of exactly the name
// asked for is the container meant, even with no incident of its own. When
// a name is folded, the answer says so.
func TestFindIncidentDoesNotFoldOntoAnotherWatchedName(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "Nginx"}, {Container: "nginx"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "nginx-1", Container: "nginx", Kind: KindDocker, DetectedAt: findBase})
	if got, err := find(t, dir, "", "Nginx"); err == nil {
		t.Errorf("Nginx = %s; nginx's incident was taken for a different container", got.ID)
	}

	if got, err := find(t, dir, "", "NGINX"); err == nil {
		t.Errorf("NGINX with Nginx and nginx watched = %s; want it refused as ambiguous", got.ID)
	}

	other := t.TempDir()
	seedIncidents(t, other, Incident{ID: "api-1", Container: "API", DetectedAt: findBase})
	got, warnings, err := FindIncident(other, "", "api")
	if err != nil || got.ID != "api-1" {
		t.Fatalf("api = %v, %v", got, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "differs from it only in case") {
		t.Errorf("warnings = %q, want one saying the name was folded", warnings)
	}
}

// A watch list that cannot be read is said to be, without its path, rather
// than read as an empty list that has no target of that name.
func TestFindIncidentAndIncidentContainerWarnAboutTheWatchList(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir, Incident{ID: "web-1", Container: "web", DetectedAt: findBase})
	if err := os.WriteFile(targetsPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warnings, err := FindIncident(dir, "", "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "watch list could not be read") || strings.Contains(warnings[0], dir) {
		t.Errorf("warnings = %q, want the watch list named as unreadable without its path", warnings)
	}
	a := IncidentContainer(dir, Incident{Container: "web"})
	if a.Container != "" || !strings.Contains(a.Why, "watch list could not be read") || a.Warning == "" || strings.Contains(a.Warning, dir) {
		t.Errorf("IncidentContainer = %+v, want the unreadable list as the reason and a warning without the path", a)
	}
}

// The incident directory that cannot be read is an error without its path.
func TestReadIncidentsKeepsThePathOut(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(incidentsDir(dir), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := FindIncident(dir, "", "web")
	if err == nil || strings.Contains(err.Error(), dir) {
		t.Errorf("err = %v, want an error without the path", err)
	}
}

// A systemd target's unit is not a name its incidents are recorded under,
// so a kind-less incident of that name is not decided by it.
func TestIncidentContainerUsesOnlyADockerTargetsUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "web", Kind: KindSystemd, Unit: "nginx"}}); err != nil {
		t.Fatal(err)
	}
	a := IncidentContainer(dir, Incident{Container: "nginx"})
	if !strings.Contains(a.Why, "no watched target has its name") {
		t.Errorf("IncidentContainer(nginx) = %+v; the systemd target's unit was taken for the incident's name", a)
	}
}
