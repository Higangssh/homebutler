package watch

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func recordingRunner(out string, err error) (CommandRunner, *[]string) {
	var calls []string
	return func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return out, err
	}, &calls
}

// The detection half for systemd and pm2 has existed since those monitors
// landed; nothing could act on an incident because the only remediation path
// called docker.Restart.
func TestRestartDispatchesByKind(t *testing.T) {
	tests := []struct {
		kind string
		name string
		want string
	}{
		{KindSystemd, "lh-elsa-monitor.service", "systemctl restart lh-elsa-monitor.service"},
		{KindPM2, "api", "pm2 restart api"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			run, calls := recordingRunner("", nil)
			out, err := Restart(tt.kind, tt.name, run)
			if err != nil {
				t.Fatalf("Restart: %v", err)
			}
			if len(*calls) != 1 || (*calls)[0] != tt.want {
				t.Errorf("ran %v, want %q", *calls, tt.want)
			}
			if !strings.Contains(out, tt.name) {
				t.Errorf("output should name the target, got %q", out)
			}
		})
	}
}

// systemctl needs root or a polkit rule, and that is the failure a homelab
// user is most likely to hit. A bare exit status leaves them guessing.
func TestRestartSystemdExplainsAPrivilegeRefusal(t *testing.T) {
	for _, output := range []string{
		"Failed to restart foo.service: Access denied",
		"Interactive authentication required.",
		"Failed to restart foo.service: Permission denied",
	} {
		run, _ := recordingRunner(output, errors.New("exit status 1"))
		_, err := Restart(KindSystemd, "foo.service", run)
		if err == nil {
			t.Fatalf("a refused restart must be an error (%q)", output)
		}
		if !strings.Contains(err.Error(), "needs root") {
			t.Errorf("refusal for %q should mention root, got: %v", output, err)
		}
	}
}

// A unit that genuinely failed to start is a different problem from one we
// were not allowed to touch, and the message should not confuse the two.
func TestRestartSystemdReportsAGenuineFailureAsItself(t *testing.T) {
	run, _ := recordingRunner("Job for foo.service failed because the control process exited", errors.New("exit status 1"))
	_, err := Restart(KindSystemd, "foo.service", run)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "needs root") {
		t.Errorf("a genuine failure should not be reported as a privilege problem: %v", err)
	}
}

func TestRestartRejectsUnknownKindAndEmptyName(t *testing.T) {
	run, calls := recordingRunner("", nil)

	if _, err := Restart("kubernetes", "pod", run); err == nil {
		t.Error("an unknown kind should be refused")
	}
	if _, err := Restart(KindSystemd, "  ", run); err == nil {
		t.Error("an empty target name should be refused")
	}
	if len(*calls) != 0 {
		t.Errorf("nothing should have been run, got %v", *calls)
	}
}

// Errors read as "not flapping". A missing incident directory is not evidence
// of a restart loop, and refusing to remediate because a read failed would be
// the wrong way to be careful.
func TestIncidentHistoryTreatsNoHistoryAsNotFlapping(t *testing.T) {
	h := IncidentHistory{Dir: t.TempDir(), Flapping: DefaultWatchConfig().Flapping}
	if h.IsFlapping("nginx") {
		t.Error("an empty incident directory is not a restart loop")
	}

	var empty IncidentHistory
	if empty.IsFlapping("nginx") {
		t.Error("an unset directory is not a restart loop")
	}
}

func TestIncidentHistoryDetectsARestartLoop(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	cfg := DefaultWatchConfig().Flapping
	for i := 0; i < cfg.ShortThreshold+1; i++ {
		inc := Incident{
			ID:         "loop-" + string(rune('a'+i)),
			Container:  "nginx",
			DetectedAt: now.Add(-time.Duration(i) * time.Minute),
		}
		if err := SaveIncident(dir, &inc, 0); err != nil {
			t.Fatalf("SaveIncident: %v", err)
		}
	}

	h := IncidentHistory{Dir: dir, Flapping: cfg, Now: func() time.Time { return now }}
	if !h.IsFlapping("nginx") {
		t.Errorf("%d restarts inside %s should count as flapping", cfg.ShortThreshold+1, cfg.ShortWindow)
	}
	if h.IsFlapping("postgres") {
		t.Error("another target's incidents must not make this one look like a loop")
	}
}
