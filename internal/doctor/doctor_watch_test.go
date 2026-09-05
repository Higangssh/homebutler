package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/notify"
	"github.com/Higangssh/homebutler/internal/watch"
)

func watchDirWith(t *testing.T, targets ...watch.Target) string {
	t.Helper()
	dir := t.TempDir()
	if len(targets) > 0 {
		if err := watch.SaveTargets(dir, targets); err != nil {
			t.Fatalf("SaveTargets: %v", err)
		}
	}
	return dir
}

func findingsFor(r *Result, category string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Category == category {
			out = append(out, f)
		}
	}
	return out
}

// The state that makes every other monitoring feature silent, and the only
// place a user finds out that watch install exists.
func TestWatchListWithNoServiceIsAWarning(t *testing.T) {
	dir := watchDirWith(t, watch.Target{Container: "nginx", AddedAt: time.Now()})
	r := &Result{}
	checkWatching(r, dir, func() (bool, string) { return false, "" })

	got := findingsFor(r, "watch")
	if len(got) != 1 || got[0].Severity != SeverityWarn {
		t.Fatalf("expected one warning, got %+v", got)
	}
	if got[0].Command != "homebutler watch install" {
		t.Errorf("the finding does not name the command that fixes it: %q", got[0].Command)
	}
	if !strings.Contains(got[0].Detail, "not whether it is running") {
		t.Errorf("the finding overstates what it checked: %q", got[0].Detail)
	}
}

func TestInstalledServiceIsAPass(t *testing.T) {
	dir := watchDirWith(t, watch.Target{Container: "nginx", AddedAt: time.Now()})
	r := &Result{}
	checkWatching(r, dir, func() (bool, string) { return true, "/home/u/.config/systemd/user/homebutler-watch.service" })

	got := findingsFor(r, "watch")
	if len(got) != 1 || got[0].Severity != SeverityPass {
		t.Fatalf("expected one pass, got %+v", got)
	}
}

// An empty watch list is not a problem. Someone who has not asked for
// monitoring is not being told they are missing it.
func TestEmptyWatchListSaysNothing(t *testing.T) {
	r := &Result{}
	checkWatching(r, watchDirWith(t), func() (bool, string) { return false, "" })
	if got := findingsFor(r, "watch"); len(got) != 0 {
		t.Errorf("an empty watch list produced %+v", got)
	}
}

func TestOpenConfigPermissionsAreAFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced the same way on Windows")
	}
	path := filepath.Join(t.TempDir(), "homebutler.yaml")
	if err := os.WriteFile(path, []byte("servers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Path: path, Servers: []config.ServerConfig{{Name: "pi", Password: "hunter2"}}}

	r := &Result{}
	checkConfigPermissions(r, cfg)

	got := findingsFor(r, "config")
	if len(got) != 1 || got[0].Severity != SeverityFail {
		t.Fatalf("expected one failure, got %+v", got)
	}
	if !strings.Contains(got[0].Command, "chmod 600") {
		t.Errorf("the finding does not name the command that fixes it: %q", got[0].Command)
	}
	if strings.Contains(got[0].Detail+got[0].Title, "hunter2") {
		t.Errorf("the finding leaked the secret it is about: %+v", got[0])
	}
}

// A config with no secrets is not a permissions problem, whatever its mode.
func TestOpenConfigWithoutSecretsIsFine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homebutler.yaml")
	if err := os.WriteFile(path, []byte("servers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Result{}
	checkConfigPermissions(r, &config.Config{Path: path})
	if got := findingsFor(r, "config"); len(got) != 0 {
		t.Errorf("a config with no secrets was flagged: %+v", got)
	}
}

func TestDockerSocketMountIsAWarning(t *testing.T) {
	inv := &inventory.Inventory{Containers: []docker.Container{
		{Name: "portainer", State: "running"},
		{Name: "nginx", State: "running"},
	}}
	inspect := func(name string) (*docker.InspectResult, error) {
		if name == "portainer" {
			return &docker.InspectResult{Mounts: []docker.Mount{
				{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
			}}, nil
		}
		return &docker.InspectResult{Mounts: []docker.Mount{{Source: "/srv/data", Destination: "/data"}}}, nil
	}

	r := &Result{}
	checkDockerSocketMounts(r, inv, inspect)

	got := findingsFor(r, "docker")
	if len(got) != 1 || got[0].Severity != SeverityWarn {
		t.Fatalf("expected one warning, got %+v", got)
	}
	if !strings.Contains(got[0].Title, "portainer") {
		t.Errorf("the finding does not name the container: %q", got[0].Title)
	}
	if !strings.Contains(got[0].Detail, "host root") {
		t.Errorf("the finding does not say what the mount grants: %q", got[0].Detail)
	}
}

// A stopped container cannot do anything with a socket it is not running on.
func TestStoppedContainerWithTheSocketIsNotFlagged(t *testing.T) {
	inv := &inventory.Inventory{Containers: []docker.Container{{Name: "old", State: "exited"}}}
	called := 0
	r := &Result{}
	checkDockerSocketMounts(r, inv, func(string) (*docker.InspectResult, error) {
		called++
		return &docker.InspectResult{Mounts: []docker.Mount{{Source: "/var/run/docker.sock"}}}, nil
	})
	if len(findingsFor(r, "docker")) != 0 || called != 0 {
		t.Errorf("a stopped container was inspected or flagged (calls=%d)", called)
	}
}

// Docker being unavailable is not a finding, and must not become N of them.
func TestInspectFailureStopsAtTheFirstContainer(t *testing.T) {
	inv := &inventory.Inventory{Containers: []docker.Container{
		{Name: "a", State: "running"}, {Name: "b", State: "running"}, {Name: "c", State: "running"},
	}}
	calls := 0
	r := &Result{}
	checkDockerSocketMounts(r, inv, func(string) (*docker.InspectResult, error) {
		calls++
		return nil, errors.New("docker daemon is not running")
	})
	if calls != 1 {
		t.Errorf("a failing collector was retried per container: %d calls", calls)
	}
	if len(findingsFor(r, "docker")) != 0 {
		t.Error("a collector failure produced a finding")
	}
}

func TestInsecureProxmoxEndpointIsAWarning(t *testing.T) {
	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{
		{Name: "pve", Insecure: true},
		{Name: "pve2", Fingerprint: "aa:bb"},
	}}
	r := &Result{}
	checkProxmoxTrust(r, cfg)

	got := findingsFor(r, "proxmox")
	if len(got) != 1 {
		t.Fatalf("expected one warning, got %+v", got)
	}
	if !strings.Contains(got[0].Title, "pve") || strings.Contains(got[0].Title, "pve2") {
		t.Errorf("the wrong endpoint was named: %q", got[0].Title)
	}
}

func TestIncidentDirectoryNearItsCap(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 9; i++ {
		at := time.Now().Add(time.Duration(i) * time.Second)
		if err := watch.SaveIncident(dir, &watch.Incident{
			ID: watch.GenerateIncidentID("nginx", at), Container: "nginx", DetectedAt: at,
		}, 0); err != nil {
			t.Fatalf("SaveIncident: %v", err)
		}
	}
	cfg := &config.Config{}
	cfg.Watch.Retention.MaxIncidents = 10

	r := &Result{}
	checkIncidentRetention(r, cfg, dir)
	got := findingsFor(r, "watch")
	if len(got) != 1 {
		t.Fatalf("nine of ten kept should be worth mentioning, got %+v", got)
	}
	if !strings.Contains(got[0].Title, "9 of 10") {
		t.Errorf("the finding does not say how full it is: %q", got[0].Title)
	}
}

// Unlimited history is a choice, not a problem.
func TestUnlimitedIncidentHistoryIsNotFlagged(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		at := time.Now().Add(time.Duration(i) * time.Second)
		_ = watch.SaveIncident(dir, &watch.Incident{ID: watch.GenerateIncidentID("n", at), Container: "n", DetectedAt: at}, 0)
	}
	cfg := &config.Config{}
	cfg.Watch.Retention.MaxIncidents = -1
	cfg.Watch.Retention.Normalize()

	r := &Result{}
	checkIncidentRetention(r, cfg, dir)
	if got := findingsFor(r, "watch"); len(got) != 0 {
		t.Errorf("explicitly unlimited history was flagged: %+v", got)
	}
}

func cfgWithChannel() *config.Config {
	cfg := &config.Config{}
	cfg.Notify.Telegram = &notify.TelegramConfig{BotToken: "t", ChatID: "c"}
	return cfg
}

// The person this fails is the one who did the work: configured a channel,
// tested it, installed the service, and is covered except for a flag nobody
// told them about.
func TestConfiguredChannelWithWatchNotificationsOffIsAWarning(t *testing.T) {
	dir := watchDirWith(t, watch.Target{Container: "nginx", AddedAt: time.Now()})
	cfg := cfgWithChannel()
	cfg.Watch.Notify.Enabled = false

	r := &Result{}
	checkWatchNotifications(r, cfg, dir)

	got := findingsFor(r, "notifications")
	if len(got) != 1 || got[0].Severity != SeverityWarn {
		t.Fatalf("expected one warning, got %+v", got)
	}
	if !strings.Contains(got[0].Detail, "nothing is sent anywhere") {
		t.Errorf("the finding does not say what does not happen: %q", got[0].Detail)
	}
}

// notify_on: flapping is the default and means a single restart is silent.
// Correct behaviour, and not something an operator can infer.
func TestEnabledNotificationsSayWhatWillNotBeSent(t *testing.T) {
	dir := watchDirWith(t, watch.Target{Container: "nginx", AddedAt: time.Now()})
	cfg := cfgWithChannel()
	cfg.Watch.Notify.Enabled = true
	cfg.Watch.Notify.NotifyOn = "flapping"

	r := &Result{}
	checkWatchNotifications(r, cfg, dir)

	got := findingsFor(r, "notifications")
	if len(got) != 1 || got[0].Severity != SeverityPass {
		t.Fatalf("expected one pass, got %+v", got)
	}
	if !strings.Contains(got[0].Detail, "single restart is recorded but not sent") {
		t.Errorf("the pass does not say what stays silent: %q", got[0].Detail)
	}
}

// checkNotifications already covers "no channel at all". Two findings about
// one gap is how a report becomes noise.
func TestNoChannelIsNotReportedTwice(t *testing.T) {
	dir := watchDirWith(t, watch.Target{Container: "nginx", AddedAt: time.Now()})
	r := &Result{}
	checkWatchNotifications(r, &config.Config{}, dir)
	if got := findingsFor(r, "notifications"); len(got) != 0 {
		t.Errorf("an unconfigured channel was reported here too: %+v", got)
	}
}

// Nothing on the watch list means nothing to be silent about.
func TestEmptyWatchListSaysNothingAboutNotifications(t *testing.T) {
	cfg := cfgWithChannel()
	r := &Result{}
	checkWatchNotifications(r, cfg, watchDirWith(t))
	if got := findingsFor(r, "notifications"); len(got) != 0 {
		t.Errorf("an empty watch list produced %+v", got)
	}
}
