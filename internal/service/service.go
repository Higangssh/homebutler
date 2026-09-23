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
	"strconv"
	"strings"
)

// Kind names the supervisor a host offers.
type Kind string

const (
	Systemd Kind = "systemd"
	Launchd Kind = "launchd"
)

// Unit describes one of the services homebutler can hand to the host's
// supervisor. There are two, and keeping them separate is the point: opening a
// port has to be something somebody asked for, never a side effect of
// installing monitoring.
type Unit struct {
	// Label names the launchd agent and the systemd unit, and doubles as the
	// filename of both.
	Label string
	// Description is what systemctl prints for the unit.
	Description string
	// Args is the homebutler subcommand the supervisor runs.
	Args []string
	// LogFile is where launchd redirects output, relative to ~/.homebutler/logs.
	// Empty means the service writes nothing worth keeping.
	LogFile string
}

// Watch is the monitoring loop, the service homebutler has always installed.
var Watch = Unit{
	Label:       "dev.homebutler.watch",
	Description: "homebutler monitoring",
	Args:        []string{"watch", "start"},
	LogFile:     "watch.log",
}

// Serve is the web dashboard, bound to host:port.
//
// The address is an argument and not a default because a supervised dashboard
// is one somebody comes back to months later: the unit has to say what it
// binds, and reading the arguments back out of it is how a report cannot
// disagree with what is actually running.
//
// No token appears here. A unit file is world-readable and a command line is
// visible in ps to every user on the machine, so the token is read from the
// config file, which doctor already checks is 0600.
func Serve(host string, port int) Unit {
	return Unit{
		Label:       "dev.homebutler.serve",
		Description: "homebutler web dashboard",
		Args:        []string{"serve", "--host", host, "--port", strconv.Itoa(port)},
		LogFile:     "serve.log",
	}
}

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

// UnitPath is where u's unit or agent file belongs for kind.
func UnitPath(kind Kind, home string, u Unit) string {
	switch kind {
	case Systemd:
		return filepath.Join(home, ".config", "systemd", "user", u.Label+".service")
	case Launchd:
		return filepath.Join(home, "Library", "LaunchAgents", u.Label+".plist")
	}
	return ""
}

// Render writes u's unit text for kind, running the binary at exe.
func Render(kind Kind, exe, home string, u Unit) string {
	switch kind {
	case Systemd:
		return systemdUnit(exe, u)
	case Launchd:
		return launchdPlist(exe, home, u)
	}
	return ""
}

// servicePATH is the search path a supervised process gets, which is not the
// one a login shell has. A launchd agent inherits a minimal PATH and would
// never find docker — it lives in /opt/homebrew/bin or /usr/local/bin on
// macOS. systemd user units are barely better.
//
// The list matches what remote.Run exports before running homebutler over SSH
// (internal/remote/ssh.go), which is the same problem in a different place.
const servicePATH = "/usr/local/bin:/usr/local/sbin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin:/snap/bin"

func systemdUnit(exe string, u Unit) string {
	return fmt.Sprintf(`[Unit]
Description=%s
Documentation=https://github.com/Higangssh/homebutler
# Docker may not be up yet at login; homebutler reconnects on its own, so this
# is a hint about ordering rather than a requirement.
After=docker.service

[Service]
Type=simple
Environment=PATH=%s
ExecStart=%s %s
# homebutler retries a dropped connection itself, so a restart here means the
# process actually died. The delay keeps a crash loop from filling the journal.
Restart=always
RestartSec=10s

[Install]
WantedBy=default.target
`, u.Description, servicePATH, exe, strings.Join(u.Args, " "))
}

func launchdPlist(exe, home string, u Unit) string {
	logPath := LogPath(Launchd, home, u)
	args := "    <string>" + exe + "</string>\n"
	for _, a := range u.Args {
		args += "    <string>" + a + "</string>\n"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <!-- launchd restarts on exit with no delay of its own; homebutler reconnects
       internally, so a restart here means the process died, and this keeps that
       from becoming a spin. -->
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, u.Label, args, servicePATH, logPath, logPath)
}

// StartCommand activates a written unit.
func StartCommand(kind Kind, path string, u Unit) []string {
	switch kind {
	case Systemd:
		return []string{"systemctl", "--user", "enable", "--now", u.Label + ".service"}
	case Launchd:
		return []string{"launchctl", "bootstrap", "gui/" + fmt.Sprint(os.Getuid()), path}
	}
	return nil
}

// StopCommand deactivates an installed unit.
func StopCommand(kind Kind, path string, u Unit) []string {
	switch kind {
	case Systemd:
		return []string{"systemctl", "--user", "disable", "--now", u.Label + ".service"}
	case Launchd:
		return []string{"launchctl", "bootout", "gui/" + fmt.Sprint(os.Getuid()) + "/" + u.Label}
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

// InstalledUnit reports whether a watch service is installed on this host and
// where its unit file is. It answers about the file, not the supervisor: a unit
// that exists and is stopped reads as installed here, and saying more would
// mean asking systemd or launchd on every call.
func InstalledUnit(u Unit) (bool, string) {
	kind, err := Detect()
	if err != nil {
		return false, ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, ""
	}
	path := UnitPath(kind, home, u)
	return Installed(path), path
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
func LogPath(kind Kind, home string, u Unit) string {
	if kind != Launchd || u.LogFile == "" {
		return ""
	}
	return filepath.Join(home, ".homebutler", "logs", u.LogFile)
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

// RestartCommand reloads a running unit, for after the watch list changes.
// The monitors read their targets once at startup, so adding a target while
// the service runs has no effect until it is restarted — and a user who is not
// told that has a container they believe is watched and is not.
func RestartCommand(kind Kind, u Unit) []string {
	switch kind {
	case Systemd:
		return []string{"systemctl", "--user", "restart", u.Label + ".service"}
	case Launchd:
		return []string{"launchctl", "kickstart", "-k", "gui/" + fmt.Sprint(os.Getuid()) + "/" + u.Label}
	}
	return nil
}

// RestartNote returns the command to pick up a changed watch list, or "" when
// no unit is installed and the next manual start will read it anyway.
func RestartNote() string {
	kind, err := Detect()
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if !Installed(UnitPath(kind, home, Watch)) {
		return ""
	}
	return strings.Join(RestartCommand(kind, Watch), " ")
}

// UnitArgs reads back the homebutler arguments a written unit runs, without the
// binary path. It returns nil when content is not a unit this package wrote.
//
// Reporting an installed service means answering "what is it actually doing",
// and the only answer that cannot drift is the one the supervisor reads. A
// port recorded anywhere else is a second copy waiting to disagree with the
// unit after somebody edits one of them.
func UnitArgs(kind Kind, content string) []string {
	switch kind {
	case Systemd:
		for _, line := range strings.Split(content, "\n") {
			if rest, ok := strings.CutPrefix(line, "ExecStart="); ok {
				fields := strings.Fields(rest)
				if len(fields) < 2 {
					return nil
				}
				return fields[1:] // drop the binary path
			}
		}
	case Launchd:
		_, rest, ok := strings.Cut(content, "<key>ProgramArguments</key>")
		if !ok {
			return nil
		}
		array, _, ok := strings.Cut(rest, "</array>")
		if !ok {
			return nil
		}
		var args []string
		for {
			_, after, ok := strings.Cut(array, "<string>")
			if !ok {
				break
			}
			value, remainder, ok := strings.Cut(after, "</string>")
			if !ok {
				break
			}
			args = append(args, value)
			array = remainder
		}
		if len(args) < 2 {
			return nil
		}
		return args[1:] // drop the binary path
	}
	return nil
}

// Address picks the --host and --port out of a unit's arguments. The bools
// report whether each was found, so a unit written by an older version — or by
// hand — reads as "not stated" rather than as 0.
func Address(args []string) (host string, port int, ok bool) {
	var haveHost, havePort bool
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--host":
			host, haveHost = args[i+1], true
		case "--port":
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				port, havePort = n, true
			}
		}
	}
	return host, port, haveHost && havePort
}
