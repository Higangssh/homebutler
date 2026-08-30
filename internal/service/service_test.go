package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The unit has to name the binary that wrote it. Rendering a bare "homebutler"
// would depend on the supervisor's PATH, which is not the shell's.
func TestRenderUsesTheAbsoluteBinaryPath(t *testing.T) {
	for _, kind := range []Kind{Systemd, Launchd} {
		got := Render(kind, "/opt/bin/homebutler", "/home/x")
		if !strings.Contains(got, "/opt/bin/homebutler") {
			t.Errorf("%s unit does not name the binary:\n%s", kind, got)
		}
	}
}

// A restart delay is what keeps a failing start from spinning. Without it the
// supervisor becomes the retry mechanism for a stream the monitor already
// retries itself.
func TestRenderSetsARestartDelay(t *testing.T) {
	if got := Render(Systemd, "/b", "/h"); !strings.Contains(got, "RestartSec=") {
		t.Errorf("systemd unit has no restart delay:\n%s", got)
	}
	if got := Render(Launchd, "/b", "/h"); !strings.Contains(got, "ThrottleInterval") {
		t.Errorf("launchd agent has no throttle:\n%s", got)
	}
}

// Both platforms are user-level: a Linux system unit would read root's home and
// find an empty watch list, and a macOS LaunchDaemon would poll a Docker
// Desktop that is not running.
func TestUnitPathsAreUserLevel(t *testing.T) {
	home := "/home/x"
	if got := UnitPath(Systemd, home); !strings.HasPrefix(got, home) {
		t.Errorf("systemd unit is not under the user's home: %s", got)
	}
	if got := UnitPath(Launchd, "/Users/x"); !strings.Contains(got, "LaunchAgents") {
		t.Errorf("launchd path is not an agent: %s", got)
	}
	if strings.Contains(UnitPath(Launchd, "/Users/x"), "LaunchDaemons") {
		t.Error("launchd path is a daemon; Docker Desktop only runs in a user session")
	}
}

// Only launchd needs its log bounded — journald rotates what systemd collects.
func TestLogPathOnlyAppliesToLaunchd(t *testing.T) {
	if got := LogPath(Systemd, "/home/x"); got != "" {
		t.Errorf("systemd should not need a log file, got %q", got)
	}
	if got := LogPath(Launchd, "/Users/x"); got == "" {
		t.Error("launchd writes a file that nothing else rotates")
	}
}

func TestTrimLogKeepsTheTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch.log")
	body := strings.Repeat("noise\n", 5000) + "LAST LINE\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := TrimLog(path, 1024); err != nil {
		t.Fatalf("TrimLog: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) > 1024 {
		t.Errorf("kept %d bytes, cap is 1024", len(got))
	}
	if !strings.HasSuffix(string(got), "LAST LINE\n") {
		t.Error("trimming dropped the end, which is the half worth keeping")
	}
	if strings.HasPrefix(string(got), "oise") {
		t.Error("trimming left a partial first line")
	}
}

func TestTrimLogLeavesSmallAndMissingFilesAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch.log")
	if err := os.WriteFile(path, []byte("short\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := TrimLog(path, 1024); err != nil {
		t.Fatalf("TrimLog: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "short\n" {
		t.Errorf("a file under the cap was rewritten: %q", got)
	}
	if err := TrimLog(filepath.Join(dir, "absent.log"), 1024); err != nil {
		t.Errorf("a missing log is not an error: %v", err)
	}
	if err := TrimLog("", 1024); err != nil {
		t.Errorf("no log path is not an error: %v", err)
	}
}

// A systemd user unit stops at logout unless lingering is enabled, and enabling
// it changes the user account rather than homebutler's own files — so it is
// reported, not done.
func TestLingerNoteIsSystemdOnly(t *testing.T) {
	if got := LingerNote(Systemd); !strings.Contains(got, "enable-linger") {
		t.Errorf("systemd note does not mention lingering: %q", got)
	}
	if got := LingerNote(Launchd); got != "" {
		t.Errorf("launchd has no lingering concept, got %q", got)
	}
}

func TestWriteCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "unit.service")
	if err := Write(path, "hello"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !Installed(path) {
		t.Error("Installed does not see the file Write just made")
	}
}
