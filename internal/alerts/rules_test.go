package alerts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRulesValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	yaml := `rules:
  - name: disk-full
    metric: disk
    threshold: 85
    action: notify
    notify: webhook
  - name: container-down
    metric: container
    watch:
      - nginx
      - vaultwarden
    action: restart
    cooldown: 5m
webhook:
  url: "https://example.com/hook"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	if len(cfg.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(cfg.Rules))
	}
	if cfg.Rules[0].Name != "disk-full" {
		t.Errorf("expected rule name 'disk-full', got %q", cfg.Rules[0].Name)
	}
	if cfg.Rules[1].Metric != "container" {
		t.Errorf("expected metric 'container', got %q", cfg.Rules[1].Metric)
	}
	if len(cfg.Rules[1].Watch) != 2 {
		t.Errorf("expected 2 watch targets, got %d", len(cfg.Rules[1].Watch))
	}
	if cfg.Webhook.URL != "https://example.com/hook" {
		t.Errorf("expected webhook URL, got %q", cfg.Webhook.URL)
	}
}

func TestLoadRulesDuplicateName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	yaml := `rules:
  - name: dup
    metric: cpu
    threshold: 80
    action: notify
  - name: dup
    metric: memory
    threshold: 80
    action: notify
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for duplicate rule name")
	}
}

func TestLoadRulesInvalidMetric(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	yaml := `rules:
  - name: bad
    metric: network
    threshold: 80
    action: notify
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for invalid metric")
	}
}

func TestLoadRulesContainerMissingWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	yaml := `rules:
  - name: bad-container
    metric: container
    action: restart
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for container metric without watch targets")
	}
}

func TestLoadRulesExecMissingCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	yaml := `rules:
  - name: bad-exec
    metric: cpu
    threshold: 80
    action: exec
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for exec action without command")
	}
}

func TestLoadRulesInvalidThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	yaml := `rules:
  - name: bad-threshold
    metric: cpu
    threshold: 150
    action: notify
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for threshold > 100")
	}
}

func TestCooldownDuration(t *testing.T) {
	r := Rule{Cooldown: "5m"}
	d := r.CooldownDuration()
	if d.Minutes() != 5 {
		t.Errorf("expected 5m, got %v", d)
	}

	r2 := Rule{Cooldown: ""}
	if r2.CooldownDuration() != 0 {
		t.Error("expected 0 for empty cooldown")
	}

	r3 := Rule{Cooldown: "invalid"}
	if r3.CooldownDuration() != 0 {
		t.Error("expected 0 for invalid cooldown")
	}
}

func TestDefaultTemplateParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	if err := os.WriteFile(path, []byte(DefaultTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRules(path)
	if err != nil {
		t.Fatalf("default template should parse: %v", err)
	}
	if len(cfg.Rules) != 4 {
		t.Errorf("expected 4 rules in default template, got %d", len(cfg.Rules))
	}
}

func TestLoadRulesFileNotFound(t *testing.T) {
	_, err := LoadRules("/nonexistent/path/alerts.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
