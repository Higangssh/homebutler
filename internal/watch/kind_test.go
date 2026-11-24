package watch

import (
	"encoding/json"
	"testing"
	"time"
)

// Every monitor records the kind that wrote the incident. incident_context
// ties report changes to a container only when the incident is known to be
// a container's, and for an incident without a kind it can only infer that
// from the watch list as it is now.
func TestEveryMonitorRecordsItsKind(t *testing.T) {
	docker := watchEvents(t, inspectRunner("2026-09-30T12:53:40Z|false", "2026-09-30T12:53:49.5Z|false"), "hb-oom-probe", piOOMDie)
	if docker.Kind != KindDocker {
		t.Errorf("docker monitor recorded kind %q", docker.Kind)
	}

	calls := 0
	systemd := func(name string, args ...string) (string, error) {
		if name != "systemctl" {
			return "", nil
		}
		calls++
		ts := "1790774000"
		if calls > 1 {
			ts = "1790774060"
		}
		return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=@" + ts, nil
	}
	unit := firstIncident(t, &SystemdMonitor{Run: systemd, Interval: 20 * time.Millisecond}, Target{Container: "u", Kind: KindSystemd, Unit: "u.service"})
	if unit.Kind != KindSystemd {
		t.Errorf("systemd monitor recorded kind %q", unit.Kind)
	}

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
	app := firstIncident(t, &PM2Monitor{Run: pm2, Interval: 20 * time.Millisecond, ReadFile: func(string) ([]byte, error) { return nil, nil }}, Target{Container: "hb-app", Kind: KindPM2, Unit: "hb-app"})
	if app.Kind != KindPM2 {
		t.Errorf("pm2 monitor recorded kind %q", app.Kind)
	}

	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "nginx", Kind: KindDocker}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dir, map[string]*ContainerState{"nginx": {Container: "nginx", RestartCount: 3, StartedAt: "2025-01-01T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	origInspect, origCapture := inspectContainerFunc, captureLogsFunc
	t.Cleanup(func() { inspectContainerFunc, captureLogsFunc = origInspect, origCapture })
	inspectContainerFunc = func(string) (*InspectResult, error) {
		return &InspectResult{RestartCount: 4, StartedAt: "2025-01-01T05:00:00Z", Running: true}, nil
	}
	captureLogsFunc = func(string, string) string { return "" }
	res, err := CheckTargets(dir, 0)
	if err != nil || len(res.Incidents) != 1 {
		t.Fatalf("watch check: %v, %d incidents", err, len(res.Incidents))
	}
	if res.Incidents[0].Kind != KindDocker {
		t.Errorf("watch check recorded kind %q", res.Incidents[0].Kind)
	}
}

// A unit that fails without starting again still reports the start of the
// run that died. Written as curr_started_at, that reads as a new run, and
// incident_context placed its window at the old start, weeks before the
// failure.
func TestSystemdFailureWithoutARestartHasNoCurrentStart(t *testing.T) {
	calls := 0
	systemd := func(name string, args ...string) (string, error) {
		if name != "systemctl" {
			return "", nil
		}
		calls++
		if calls == 1 {
			return "ActiveState=active\nSubState=running\nExecMainStartTimestamp=@1790774000", nil
		}
		return "ActiveState=failed\nSubState=failed\nExecMainStartTimestamp=@1790774000\nExecMainStatus=1", nil
	}
	inc := firstIncident(t, &SystemdMonitor{Run: systemd, Interval: 20 * time.Millisecond}, Target{Container: "u", Kind: KindSystemd, Unit: "u.service"})
	if inc.PrevStarted == "" {
		t.Error("prev_started_at left out; the run that died is known")
	}
	if inc.CurrStarted != "" {
		t.Errorf("curr_started_at = %q for a unit that did not start again, want empty", inc.CurrStarted)
	}
}
