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
		got := Render(kind, "/opt/bin/homebutler", "/home/x", Watch)
		if !strings.Contains(got, "/opt/bin/homebutler") {
			t.Errorf("%s unit does not name the binary:\n%s", kind, got)
		}
	}
}

// A restart delay is what keeps a failing start from spinning. Without it the
// supervisor becomes the retry mechanism for a stream the monitor already
// retries itself.
func TestRenderSetsARestartDelay(t *testing.T) {
	if got := Render(Systemd, "/b", "/h", Watch); !strings.Contains(got, "RestartSec=") {
		t.Errorf("systemd unit has no restart delay:\n%s", got)
	}
	if got := Render(Launchd, "/b", "/h", Watch); !strings.Contains(got, "ThrottleInterval") {
		t.Errorf("launchd agent has no throttle:\n%s", got)
	}
}

// Both platforms are user-level: a Linux system unit would read root's home and
// find an empty watch list, and a macOS LaunchDaemon would poll a Docker
// Desktop that is not running.
func TestUnitPathsAreUserLevel(t *testing.T) {
	home := "/home/x"
	if got := UnitPath(Systemd, home, Watch); !strings.HasPrefix(got, home) {
		t.Errorf("systemd unit is not under the user's home: %s", got)
	}
	if got := UnitPath(Launchd, "/Users/x", Watch); !strings.Contains(got, "LaunchAgents") {
		t.Errorf("launchd path is not an agent: %s", got)
	}
	if strings.Contains(UnitPath(Launchd, "/Users/x", Watch), "LaunchDaemons") {
		t.Error("launchd path is a daemon; Docker Desktop only runs in a user session")
	}
}

// Only launchd needs its log bounded — journald rotates what systemd collects.
func TestLogPathOnlyAppliesToLaunchd(t *testing.T) {
	if got := LogPath(Systemd, "/home/x", Watch); got != "" {
		t.Errorf("systemd should not need a log file, got %q", got)
	}
	if got := LogPath(Launchd, "/Users/x", Watch); got == "" {
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

// A supervised process does not get a login shell's PATH. Without this a
// launchd agent never finds docker, which lives in /opt/homebrew/bin or
// /usr/local/bin — the unit installs, runs, and monitors nothing.
func TestRenderSetsAPathThatCanFindDocker(t *testing.T) {
	for _, kind := range []Kind{Systemd, Launchd} {
		got := Render(kind, "/b", "/h", Watch)
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
			if !strings.Contains(got, dir) {
				t.Errorf("%s unit's PATH does not include %s:\n%s", kind, dir, got)
			}
		}
	}
}

// Reinstalling has to unload before it loads: launchctl bootstrap fails on an
// already-loaded service, so overwriting the file alone leaves the old
// configuration live.
func TestStopCommandExistsForEveryKind(t *testing.T) {
	for _, kind := range []Kind{Systemd, Launchd} {
		if len(StopCommand(kind, "/p", Watch)) == 0 {
			t.Errorf("%s has no way to unload, so --force cannot reload it", kind)
		}
		if len(RestartCommand(kind, Watch)) == 0 {
			t.Errorf("%s has no restart command, so a changed watch list cannot be picked up", kind)
		}
	}
}

// The address a report prints has to be the one the supervisor runs, so it is
// read back out of the unit rather than remembered somewhere else. That only
// works if what Render writes is what UnitArgs can read — on both platforms,
// and with a --config appended after the address.
func TestTheAddressSurvivesARoundTripThroughTheUnit(t *testing.T) {
	for _, kind := range []Kind{Systemd, Launchd} {
		unit := Serve("0.0.0.0", 9090)
		unit.Args = append(unit.Args, "--config", "/etc/homebutler.yaml")

		args := UnitArgs(kind, Render(kind, "/usr/local/bin/homebutler", "/home/x", unit))
		if len(args) == 0 {
			t.Errorf("%s: read no arguments back out of the unit", kind)
			continue
		}
		if args[0] != "serve" {
			t.Errorf("%s: first argument is %q, want serve — the binary path was not dropped", kind, args[0])
		}
		host, port, ok := Address(args)
		if !ok {
			t.Errorf("%s: unit states no address: %v", kind, args)
			continue
		}
		if host != "0.0.0.0" || port != 9090 {
			t.Errorf("%s: unit serves %s:%d, want 0.0.0.0:9090", kind, host, port)
		}
	}
}

// A unit that predates --host and --port, or one edited by hand, has to read
// as "does not say" rather than as port 0.
func TestAnAddressIsNotInventedForAUnitThatLacksOne(t *testing.T) {
	for _, kind := range []Kind{Systemd, Launchd} {
		args := UnitArgs(kind, Render(kind, "/b", "/h", Watch))
		if _, _, ok := Address(args); ok {
			t.Errorf("%s: the watch unit was read as stating an address: %v", kind, args)
		}
	}
}

// Installing the dashboard must not overwrite the monitor, and stopping one
// must not stop the other.
func TestTheTwoServicesAreSeparateUnits(t *testing.T) {
	serve := Serve("127.0.0.1", 8080)
	if serve.Label == Watch.Label {
		t.Fatalf("both services use the label %q", serve.Label)
	}
	for _, kind := range []Kind{Systemd, Launchd} {
		if UnitPath(kind, "/home/x", serve) == UnitPath(kind, "/home/x", Watch) {
			t.Errorf("%s: both services write to %s", kind, UnitPath(kind, "/home/x", serve))
		}
		if LogPath(Launchd, "/home/x", serve) == LogPath(Launchd, "/home/x", Watch) {
			t.Errorf("%s: both services log to the same file", kind)
		}
	}
}
