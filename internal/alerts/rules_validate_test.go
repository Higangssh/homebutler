package alerts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Higangssh/homebutler/internal/config"
)

// watch reads the config file a second time, into UserConfig, and config
// validate checks it against config.Config. When the two disagreed, validate
// called alerts.rules an unknown key that had no effect while watch was acting
// on it (#338). Every key UserConfig reads has to be one validate accepts.
func TestValidateAcceptsEveryKeyWatchReads(t *testing.T) {
	var uc UserConfig
	uc.Watch.Enabled = true
	uc.Watch.NotifyOn = "all"
	uc.Watch.Cooldown = "5m"
	uc.Alerts.CPU, uc.Alerts.Memory, uc.Alerts.Disk = 90, 85, 90
	uc.Alerts.Rules = []Rule{{
		Name: "web", Metric: "container", Watch: []string{"nginx"}, Kind: "docker",
		Action: "notify", Exec: "true", Timeout: "30s", Notify: "webhook",
		Duration: "1m", Threshold: 1, Cooldown: "5m", MaxRetries: 1,
	}}
	data, err := yaml.Marshal(uc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, f := range config.Validate(path).Findings {
		if strings.Contains(f.Message, "not found") || f.Severity == config.SeverityError {
			t.Errorf("validate rejects something watch reads: %s %s\n%s", f.Field, f.Message, data)
		}
	}
}

// A typo inside a rule is still caught, at its line.
func TestValidateReportsAnUnknownKeyInsideARule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "alerts:\n  rules:\n    - name: web\n      metric: container\n      watch: [nginx]\n      acton: notify\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var unknown, broken bool
	for _, f := range config.Validate(path).Findings {
		if f.Field == "alerts.rules[0].acton" && strings.Contains(f.Message, "Line 6") {
			unknown = true
		}
		if f.Severity == config.SeverityError && strings.Contains(f.Message, "unknown action") {
			broken = true
		}
	}
	if !unknown || !broken {
		t.Fatalf("want the misspelt key at line 6 and the missing action as an error, got unknown=%v broken=%v", unknown, broken)
	}
}
