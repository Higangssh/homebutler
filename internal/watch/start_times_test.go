package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// prev_started_at and curr_started_at are RFC 3339 in UTC, or empty. The three
// monitors learn the time three ways and used to write each as it came:
// Docker's RFC 3339, systemd's "Wed 2026-09-30 21:46:44 KST", and for pm2 a
// sentence about restart counts. A caller parses the field as one thing.
var startTimeShape = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z$`)

func assertStartTime(t *testing.T, who, field, value string) {
	t.Helper()
	if value != "" && !startTimeShape.MatchString(value) {
		t.Errorf("%s %s = %q, which is neither RFC 3339 UTC nor empty", who, field, value)
	}
}

func TestEveryMonitorWritesStartTimesInOneShape(t *testing.T) {
	// Docker: the Pi's recorded die, with inspect before and after a restart.
	inc := watchEvents(t, inspectRunner("2026-09-30T12:53:40Z|false", "2026-09-30T12:53:49.5Z|false"), "hb-oom-probe", piOOMDie)
	assertStartTime(t, "docker", "prev_started_at", inc.PrevStarted)
	assertStartTime(t, "docker", "curr_started_at", inc.CurrStarted)
	if inc.PrevStarted == "" || inc.CurrStarted == "" {
		t.Errorf("docker left a known time out: prev=%q curr=%q", inc.PrevStarted, inc.CurrStarted)
	}

	// systemd, in the default format a systemd older than 248 prints.
	calls := 0
	systemd := func(name string, args ...string) (string, error) {
		if name != "systemctl" {
			return "", nil
		}
		for _, a := range args {
			if a == "--timestamp=unix" {
				return "", fmt.Errorf("unrecognized option '--timestamp=unix'")
			}
		}
		calls++
		ts := "Wed 2026-09-30 12:46:44 UTC"
		if calls > 1 {
			ts = "Wed 2026-09-30 12:47:44 UTC"
		}
		return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=" + ts, nil
	}
	inc = firstIncident(t, &SystemdMonitor{Run: systemd, Interval: 20 * time.Millisecond}, Target{Container: "u", Kind: "systemd", Unit: "u.service"})
	assertStartTime(t, "systemd", "prev_started_at", inc.PrevStarted)
	assertStartTime(t, "systemd", "curr_started_at", inc.CurrStarted)
	if inc.PrevStarted != "2026-09-30T12:46:44Z" || inc.CurrStarted != "2026-09-30T12:47:44Z" {
		t.Errorf("systemd on the fallback: prev=%q curr=%q", inc.PrevStarted, inc.CurrStarted)
	}

	// pm2, from the jlist fields a real pm2 printed around a restart.
	polls := 0
	pm2 := func(name string, args ...string) (string, error) {
		polls++
		env := pm2Env{RestartTime: 0, Status: "online", PMUptime: 1790774044807}
		if polls > 1 {
			env = pm2Env{RestartTime: 1, Status: "online", PMUptime: 1790774047531}
		}
		out, _ := json.Marshal([]pm2Process{{Name: "hb-app", PM2Env: env}})
		return string(out), nil
	}
	inc = firstIncident(t, &PM2Monitor{Run: pm2, Interval: 20 * time.Millisecond, ReadFile: func(string) ([]byte, error) { return nil, nil }}, Target{Container: "hb-app", Kind: "pm2", Unit: "hb-app"})
	assertStartTime(t, "pm2", "prev_started_at", inc.PrevStarted)
	assertStartTime(t, "pm2", "curr_started_at", inc.CurrStarted)
	if strings.Contains(inc.PrevStarted+inc.CurrStarted, "restart_time") {
		t.Errorf("pm2 still writes a sentence: %q %q", inc.PrevStarted, inc.CurrStarted)
	}
}

type monitor interface {
	Watch(ctx context.Context, targets []Target, incidents chan<- Incident) error
}

func firstIncident(t *testing.T, m monitor, target Target) Incident {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	incidents := make(chan Incident, 4)
	go func() { _ = m.Watch(ctx, []Target{target}, incidents) }()
	select {
	case inc := <-incidents:
		return inc
	case <-ctx.Done():
		t.Fatal("no incident")
		return Incident{}
	}
}
