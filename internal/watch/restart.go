package watch

import (
	"fmt"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

// Restart restarts a target of the given kind.
//
// The monitors have been able to detect systemd and pm2 incidents since those
// landed, but nothing could act on one: the only remediation path called
// docker.Restart, so `action: restart` on a systemd unit failed with a docker
// error about a container that does not exist.
//
// run is the command runner, matching the monitors. A nil run uses the real
// one, so callers that are not testing do not have to supply it.
func Restart(kind, name string, run CommandRunner) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("no target name to restart")
	}
	if run == nil {
		run = util.RunCmd
	}

	switch kind {
	case KindSystemd:
		return restartSystemd(name, run)
	case KindPM2:
		return restartPM2(name, run)
	default:
		return "", fmt.Errorf("cannot restart %q: unknown target kind %q", name, kind)
	}
}

func restartSystemd(unit string, run CommandRunner) (string, error) {
	out, err := run("systemctl", "restart", unit)
	if err != nil {
		// systemctl needs root or a polkit rule. Saying so is the difference
		// between a user fixing their setup and one reading a bare exit
		// status, and this is the failure they are most likely to hit.
		if isPrivilegeRefusal(out, err) {
			return out, fmt.Errorf("restarting %s needs root: run homebutler as root or grant a polkit rule (%v)", unit, err)
		}
		return out, fmt.Errorf("restarting %s: %v: %s", unit, err, out)
	}
	return fmt.Sprintf("systemctl restart %s", unit), nil
}

func restartPM2(app string, run CommandRunner) (string, error) {
	out, err := run("pm2", "restart", app)
	if err != nil {
		return out, fmt.Errorf("restarting %s: %v: %s", app, err, out)
	}
	return fmt.Sprintf("pm2 restart %s", app), nil
}

// isPrivilegeRefusal recognises the ways systemctl reports that it was not
// allowed to do the thing, rather than that the thing failed.
func isPrivilegeRefusal(out string, err error) bool {
	if util.IsPermissionError(err) {
		return true
	}
	lower := strings.ToLower(out)
	return strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "interactive authentication required") ||
		strings.Contains(lower, "authentication is required")
}

// IncidentHistory answers whether a target is flapping, from the incidents
// already recorded on disk. It satisfies the FlapChecker that the alerts
// playbook consults before restarting anything.
type IncidentHistory struct {
	Dir      string
	Flapping FlappingConfig
	// Now is for tests. Nil uses time.Now.
	Now func() time.Time
}

// IsFlapping reports whether target has restarted often enough recently to
// count as a loop. Errors read as "not flapping": a missing or unreadable
// incident directory is not evidence of a loop, and refusing to remediate on
// the strength of a read error would be the wrong way to be careful.
func (h IncidentHistory) IsFlapping(target string) bool {
	if h.Dir == "" {
		return false
	}
	// Refs, not incidents: the rules engine asks this before every remediation,
	// and reading captured logs to count restarts is work the filename already
	// answers.
	refs, err := ListIncidentRefs(h.Dir)
	if err != nil || len(refs) == 0 {
		return false
	}
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	cfg := h.Flapping
	return cfg.CheckRefs(target, refs, now()).IsFlapping
}
