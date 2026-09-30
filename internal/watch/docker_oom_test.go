package watch

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Recorded on a Raspberry Pi with Docker 29.4.2: a container given 32 MB
// that allocates until the kernel stops it, and a container stopped with
// docker kill. Both exit 137. Only the first is preceded by an oom event,
// 0.46s before its die. Docker 29 sends Action and leaves status empty.
const (
	piOOM       = `{"Type":"container","Action":"oom","Actor":{"ID":"4f1c","Attributes":{"image":"alpine","name":"hb-oom-probe"}},"scope":"local","time":1790772823,"timeNano":1790772823908813577}`
	piOOMDie    = `{"Type":"container","Action":"die","Actor":{"ID":"4f1c","Attributes":{"execDuration":"1","exitCode":"137","image":"alpine","name":"hb-oom-probe"}},"scope":"local","time":1790772824,"timeNano":1790772824373712621}`
	piKillDie   = `{"Type":"container","Action":"die","Actor":{"ID":"9a2e","Attributes":{"execDuration":"2","exitCode":"137","image":"alpine","name":"hb-kill-probe"}},"scope":"local","time":1790772827,"timeNano":1790772827200404022}`
	piDiedAtRFC = "2026-09-30T12:53:44.373712621Z" // piOOMDie's timeNano
)

// inspectRunner answers docker inspect with each of states in turn, and
// anything else with logs.
func inspectRunner(states ...string) CommandRunner {
	calls := 0
	return func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			if calls >= len(states) {
				return "", context.DeadlineExceeded
			}
			calls++
			return states[calls-1], nil
		}
		return "logs", nil
	}
}

func watchEvents(t *testing.T, run CommandRunner, container string, lines ...string) Incident {
	t.Helper()
	dm := &DockerMonitor{Run: run, PostLogDelay: time.Millisecond, Events: fakeEventStream(lines...)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	incidents := make(chan Incident, 4)
	go func() { _ = dm.watchOnce(ctx, []Target{{Container: container, Kind: "docker"}}, incidents) }()
	select {
	case inc := <-incidents:
		return inc
	case <-ctx.Done():
		t.Fatal("no incident")
		return Incident{}
	}
}

func TestAnOOMEventBeforeTheDieIsWhatMakesItOOM(t *testing.T) {
	inc := watchEvents(t, inspectRunner(), "hb-oom-probe", piOOM, piOOMDie)
	if !inc.OOMKilled {
		t.Fatal("oom then die was not recorded as OOM-killed")
	}
	got := Analyze(CrashInfo{ExitCode: *inc.ExitCode, OOMKilled: inc.OOMKilled})
	if got.Category != "oom" || got.Confidence != "high" {
		t.Errorf("classified as %s/%s, want oom/high", got.Category, got.Confidence)
	}
}

func TestADieWithoutAnOOMEventIsNotOOM(t *testing.T) {
	inc := watchEvents(t, inspectRunner(), "hb-kill-probe", piKillDie)
	if inc.OOMKilled {
		t.Fatal("docker kill was recorded as OOM-killed")
	}
	got := Analyze(CrashInfo{ExitCode: *inc.ExitCode, OOMKilled: inc.OOMKilled})
	if got.Category != "unknown" || got.Confidence != "low" || got.Signal != "SIGKILL" {
		t.Errorf("classified as %s/%s/%s, want unknown/low/SIGKILL", got.Category, got.Confidence, got.Signal)
	}
}

// The OOM killer can take one process and leave the container running, so an
// oom event is only about the die that follows it closely, and only for the
// same container.
func TestAnOOMEventOnlyExplainsItsOwnDieSoonAfter(t *testing.T) {
	stale := strings.Replace(piOOM, "1790772823908813577", "1790772700000000000", 1)
	if inc := watchEvents(t, inspectRunner(), "hb-oom-probe", stale, piOOMDie); inc.OOMKilled {
		t.Error("an oom two minutes before the die was taken as its cause")
	}
	// hb-oom-probe's oom, then hb-kill-probe's die: 3.3s apart and inside
	// the window, but another container's.
	if inc := watchEvents(t, inspectRunner(), "hb-kill-probe", piOOM, piKillDie); inc.OOMKilled {
		t.Error("another container's oom was taken as the cause of this die")
	}
}

// Inspect is the second source: it catches an OOM whose event was missed, for
// as long as nothing has restarted the container.
func TestInspectCanSayOOMWhenTheEventWasMissed(t *testing.T) {
	inc := watchEvents(t, inspectRunner("2026-09-30T12:53:40Z|true"), "hb-oom-probe", piOOMDie)
	if !inc.OOMKilled {
		t.Error("inspect said OOMKilled=true and the incident did not")
	}
}

// Start fields hold times or nothing. They used to hold "died at event time
// 1790772824" and "(post-restart)", the second whether or not anything had
// restarted.
func TestStartFieldsHoldTimesOrNothing(t *testing.T) {
	cases := []struct {
		name       string
		states     []string
		prev, curr string
	}{
		{"not restarted", []string{"2026-09-30T12:53:40Z|false", "2026-09-30T12:53:40Z|false"}, "2026-09-30T12:53:40Z", ""},
		{"restarted", []string{"2026-09-30T12:53:40Z|false", "2026-09-30T12:53:49.5Z|false"}, "2026-09-30T12:53:40Z", "2026-09-30T12:53:49.5Z"},
		// Already restarted by the time it was first read: that start is the
		// next run's, not the one that died.
		{"restarted before the first read", []string{"2026-09-30T12:53:45Z|false", "2026-09-30T12:53:45Z|false"}, "", "2026-09-30T12:53:45Z"},
		{"inspect failed", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inc := watchEvents(t, inspectRunner(c.states...), "hb-oom-probe", piOOMDie)
			if inc.PrevStarted != c.prev || inc.CurrStarted != c.curr {
				t.Errorf("prev=%q curr=%q, want prev=%q curr=%q (died at %s)", inc.PrevStarted, inc.CurrStarted, c.prev, c.curr, piDiedAtRFC)
			}
		})
	}
}
