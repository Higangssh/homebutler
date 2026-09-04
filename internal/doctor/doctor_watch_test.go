package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
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
