package watch

import (
	"strings"
	"testing"
	"time"
)

var findBase = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)

// A target means its latest incident, not the first one found on disk.
func TestFindIncidentByTargetIsTheLatest(t *testing.T) {
	dir := t.TempDir()
	seedIncidents(t, dir,
		Incident{ID: "paperless-old", Container: "paperless", DetectedAt: findBase, PreLogs: "old"},
		Incident{ID: "paperless-new", Container: "paperless", DetectedAt: findBase.Add(time.Hour), PreLogs: "new"},
		Incident{ID: "redis-newest", Container: "redis", DetectedAt: findBase.Add(2 * time.Hour)},
	)

	got, err := FindIncident(dir, "", "paperless")
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
		{"", "-x", "invalid target"},
		{"-x", "", "invalid incident ID"},
	} {
		if _, err := FindIncident(dir, tc.id, tc.target); err == nil || !strings.Contains(err.Error(), tc.want) {
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
	got, err := FindIncident(dir, "", "web")
	if err != nil || got.ID != "web-lower" {
		t.Errorf("web = %v, %v; want web's own incident although Web's is newer", got, err)
	}
	got, err = FindIncident(dir, "", "api")
	if err != nil || got.ID != "api-only" {
		t.Errorf("api = %v, %v; want the one name that differs only in case", got, err)
	}
	if _, err := FindIncident(dir, "", "WEB"); err == nil || !strings.Contains(err.Error(), "differ only in case") {
		t.Errorf("WEB with web and Web recorded: err = %v, want it refused as ambiguous", err)
	}
}

// watch list shows a target by its own name, and a docker target added with
// a unit is watched, and recorded, under the unit.
func TestFindIncidentResolvesATargetWatchedUnderAUnit(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "blog", Unit: "paperless"}}); err != nil {
		t.Fatal(err)
	}
	seedIncidents(t, dir, Incident{ID: "paperless-1", Container: "paperless", DetectedAt: findBase})
	got, err := FindIncident(dir, "", "blog")
	if err != nil || got.ID != "paperless-1" {
		t.Errorf("blog = %v, %v; want the incident recorded under its unit", got, err)
	}
}

// Only a container can have report changes tied to it, and only when that
// is known: the incident's own kind, or one watched target of one kind.
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
		inc     Incident
		want    string
		whyHint string
	}{
		{Incident{Container: "paperless"}, "paperless", ""},
		{Incident{Container: "caddy.service"}, "", "systemd"},
		{Incident{Container: "api"}, "", "pm2"},
		{Incident{Container: "pve/100", Source: "proxmox"}, "", "proxmox"},
		{Incident{Container: "gone"}, "", "no watched target has its name"},
		{Incident{Container: "web"}, "", "more than one kind"},
		// A recorded kind settles it, whatever the list says.
		{Incident{Container: "web", Kind: KindDocker}, "web", ""},
		{Incident{Container: "web", Kind: KindSystemd}, "", "systemd"},
		{Incident{Container: "gone", Kind: KindDocker}, "gone", ""},
	} {
		name, why := IncidentContainer(dir, tc.inc)
		if name != tc.want || !strings.Contains(why, tc.whyHint) || (name == "") == (why == "") {
			t.Errorf("IncidentContainer(%q, kind %q, source %q) = %q, %q; want %q and a reason mentioning %q",
				tc.inc.Container, tc.inc.Kind, tc.inc.Source, name, why, tc.want, tc.whyHint)
		}
	}
}
