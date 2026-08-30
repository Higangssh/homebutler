// Package service registers homebutler's monitoring loop with the supervisor
// the host already has, so nothing here is a daemon of its own.
//
// Both platforms are addressed at the user level, and neither is a preference:
//
// On Linux, WatchDir resolves ~/.homebutler/watch from the invoking user's home
// directory. A system unit runs as root, reads /root/.homebutler/watch, finds
// nothing there, and monitors an empty list without saying so. Lingering is what
// makes a user unit outlive the session.
//
// On macOS, Docker Desktop only runs inside a logged-in user session, so a
// LaunchDaemon would poll a daemon that is not there.
package service

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Kind names the supervisor a host offers.
type Kind string

const (
	Systemd Kind = "systemd"
	Launchd Kind = "launchd"
)

// Label is the unit and agent name. It doubles as the plist filename.
const Label = "dev.homebutler.watch"

// Plan is what Install would do, and what Status reports after it has.
type Plan struct {
	Kind Kind   `json:"kind"`
	Path string `json:"path"`
	// Start is the command that activates the written unit, shown so an
	// operator can see what was run on their behalf.
	Start []string `json:"start"`
}

// Detect reports which supervisor this host offers, or an error naming what was
// looked for. A host with neither is not a failure to handle quietly: telling
// someone monitoring is installed when nothing supervises it would be worse
// than refusing.
func Detect() (Kind, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("systemctl"); err != nil {
			return "", fmt.Errorf("systemctl not found; homebutler can only install a service where systemd runs it")
		}
		return Systemd, nil
	case "darwin":
		if _, err := exec.LookPath("launchctl"); err != nil {
			return "", fmt.Errorf("launchctl not found, which should not happen on macOS")
		}
		return Launchd, nil
	default:
		return "", fmt.Errorf("no supported supervisor on %s; homebutler installs a systemd user unit or a launchd agent", runtime.GOOS)
	}
}

// UnitPath is where the unit or agent file belongs for kind.
func UnitPath(kind Kind, home string) string {
	switch kind {
	case Systemd:
		return filepath.Join(home, ".config", "systemd", "user", Label+".service")
	case Launchd:
		return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
	}
	return ""
}

// Render writes the unit text for kind, running the binary at exe.
func Render(kind Kind, exe, home string) string {
	switch kind {
	case Systemd:
		return systemdUnit(exe)
	case Launchd:
		return launchdPlist(exe, home)
	}
	return ""
}

func systemdUnit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=homebutler monitoring
Documentation=https://github.com/Higangssh/homebutler
# Docker may not be up yet at login; the monitor reconnects on its own, so this
# is a hint about ordering rather than a requirement.
After=docker.service

[Service]
Type=simple
ExecStart=%s watch start
# The monitor retries a dropped event stream itself, so a restart here means the
# process actually died. The delay keeps a crash loop from filling the journal.
Restart=always
RestartSec=10s

[Install]
WantedBy=default.target
`, exe)
}

func launchdPlist(exe, home string) string {
	logDir := filepath.Join(home, ".homebutler", "logs")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>watch</string>
    <string>start</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <!-- launchd restarts on exit with no delay of its own; the monitor reconnects
       internally, so a restart here means the process died, and this keeps that
       from becoming a spin. -->
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>%s/watch.log</string>
  <key>StandardErrorPath</key>
  <string>%s/watch.log</string>
</dict>
</plist>
`, Label, exe, logDir, logDir)
}

// StartCommand activates a written unit.
func StartCommand(kind Kind, path string) []string {
	switch kind {
	case Systemd:
		return []string{"systemctl", "--user", "enable", "--now", Label + ".service"}
	case Launchd:
		return []string{"launchctl", "bootstrap", "gui/" + fmt.Sprint(os.Getuid()), path}
	}
	return nil
}

// StopCommand deactivates an installed unit.
func StopCommand(kind Kind, path string) []string {
	switch kind {
	case Systemd:
		return []string{"systemctl", "--user", "disable", "--now", Label + ".service"}
	case Launchd:
		return []string{"launchctl", "bootout", "gui/" + fmt.Sprint(os.Getuid()) + "/" + Label}
	}
	return nil
}

// LingerNote returns the advice a systemd user unit needs to outlive the
// session, or "" where it does not apply. Reported rather than done: enabling
// lingering is a change to the user account, not to homebutler's own files.
func LingerNote(kind Kind) string {
	if kind != Systemd {
		return ""
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "$USER"
	}
	return fmt.Sprintf("A user unit stops at logout unless lingering is enabled:\n    sudo loginctl enable-linger %s", user)
}

// Write puts the rendered unit at path, creating the directory it belongs in.
func Write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Installed reports whether a unit file is already present at path.
func Installed(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Run executes an activation command, returning its combined output on failure
// so the operator sees what the supervisor said rather than an exit code.
func Run(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("no command to run")
	}
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// MaxLogBytes bounds the file launchd redirects this process's output into.
//
// systemd sends stderr to journald, which rotates it. launchd writes
// StandardErrorPath and rotates nothing, and macOS offers no rotation for it
// without a newsyslog.d entry, which needs root. Bounding it here needs neither.
const MaxLogBytes = 4 << 20

// LogPath is the file the launchd agent redirects output into. Empty on
// platforms whose supervisor already handles this.
func LogPath(kind Kind, home string) string {
	if kind != Launchd {
		return ""
	}
	return filepath.Join(home, ".homebutler", "logs", "watch.log")
}

// TrimLog truncates path to its last max bytes, keeping the end, and does
// nothing if the file is smaller or absent.
//
// Rewriting the file underneath the supervisor is safe because launchd opens
// these paths for append: the next write goes to the end of the file as it is
// then, not to a remembered offset.
func TrimLog(path string, max int64) error {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() <= max {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(info.Size()-max, 0); err != nil {
		return err
	}
	kept, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	// Start at a line boundary so the first surviving line is whole.
	if i := indexByte(kept, '\n'); i >= 0 && i < len(kept)-1 {
		kept = kept[i+1:]
	}
	return os.WriteFile(path, kept, info.Mode().Perm())
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
