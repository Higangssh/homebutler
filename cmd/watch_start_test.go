package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
)

// watch start used to return when the watch list was empty. Under a supervisor
// that restarts on exit, that is a loop: the process starts, prints one line,
// exits, and is started again every throttle interval.
//
// It is also wrong on its own merits since #97 — thresholds and remediation
// rules run in this process and neither needs a watch target.
func TestWatchStartDoesNotExitOnAnEmptyWatchList(t *testing.T) {
	src := readSource(t, "watch.go")

	// The empty-list branch must not return; it should say what it is doing.
	i := strings.Index(src, "Nothing on the watch list")
	if i < 0 {
		t.Fatal("watch start no longer reports an empty watch list at all")
	}
	// Look at the few lines that follow the message.
	tail := src[i:]
	if end := strings.Index(tail, "// Load watch config"); end > 0 {
		tail = tail[:end]
	}
	if strings.Contains(tail, "return nil") {
		t.Error("watch start still returns on an empty watch list, which a supervisor turns into a restart loop")
	}
}

// When the rules will not load, watch start still runs the threshold watcher
// from config.yaml's alerts block. The warning used to say it was continuing
// "without thresholds or rules", which sent people looking for a threshold
// problem they did not have (#342). config validate describes the same failure
// and the two should not disagree.
//
// The warning is built from what loadAlertsConfig actually returns, so a
// change in where the rules come from shows up here.
func TestWatchStartRulesWarningSaysThresholdsStillRun(t *testing.T) {
	src := readSource(t, "watch.go")
	if !strings.Contains(src, "alertsLoadWarning(alertsErr)") {
		t.Fatal("watch start no longer prints alertsLoadWarning when the rules fail to load")
	}

	badRule := "alerts:\n  rules:\n    - name: disk-cleanup\n      action: purge\n"
	cases := []struct {
		name       string
		configYAML string
		alertsYAML string // written to ~/.homebutler/alerts.yaml when set
		wantFile   bool   // whether the warning should say alerts.yaml channels are lost
	}{
		{
			// The issue's case: rules in config.yaml, no notify block, no
			// alerts.yaml. alerts.yaml is never read, so nothing from it is lost.
			name:       "bad rule in config.yaml",
			configYAML: badRule,
		},
		{
			name:       "bad rule in config.yaml with notify",
			configYAML: badRule + "notify:\n  ntfy:\n    topic: t\n",
			alertsYAML: "notify:\n  ntfy:\n    topic: legacy\n",
		},
		{
			// config.yaml has neither rules nor channels, so the legacy file
			// is the source of both, and a failure loses both.
			name:       "bad legacy alerts.yaml",
			configYAML: "alerts:\n  cpu: 90\n",
			alertsYAML: "rules: [this is not: valid\n",
			wantFile:   true,
		},
	}

	oldCfg := cfg
	t.Cleanup(func() { cfg = oldCfg })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if tc.alertsYAML != "" {
				writeFile(t, filepath.Join(home, ".homebutler", "alerts.yaml"), tc.alertsYAML)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeFile(t, path, tc.configYAML)
			loaded, err := config.Load(path)
			if err != nil {
				t.Fatalf("loading config.yaml: %v", err)
			}
			cfg = loaded

			_, loadErr := loadAlertsConfig("")
			if loadErr == nil {
				t.Fatal("loadAlertsConfig succeeded; the case does not exercise a failure")
			}
			msg := alertsLoadWarning(loadErr)

			if strings.Contains(msg, "without thresholds") {
				t.Errorf("warning still says thresholds are off, but alerts.Watch runs regardless:\n%s", msg)
			}
			if !strings.Contains(msg, loadErr.Error()) {
				t.Errorf("warning dropped the load error:\n%s", msg)
			}
			if !strings.Contains(msg, alerts.ChecksStillRun) {
				t.Errorf("warning does not say %q, as config validate does:\n%s", alerts.ChecksStillRun, msg)
			}
			if got := strings.Contains(msg, "notification channels"); got != tc.wantFile {
				t.Errorf("warning mentions lost notification channels = %v, want %v:\n%s", got, tc.wantFile, msg)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
