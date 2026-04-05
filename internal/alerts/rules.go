package alerts

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Rule defines a single alert rule from the YAML configuration.
type Rule struct {
	Name       string   `yaml:"name" json:"name"`
	Metric     string   `yaml:"metric" json:"metric"`       // "cpu", "memory", "disk", "container"
	Threshold  float64  `yaml:"threshold" json:"threshold"` // percentage for cpu/memory/disk
	Duration   string   `yaml:"duration,omitempty" json:"duration,omitempty"`
	Watch      []string `yaml:"watch,omitempty" json:"watch,omitempty"` // container names
	Action     string   `yaml:"action" json:"action"`                   // "notify", "restart", "exec"
	Exec       string   `yaml:"exec,omitempty" json:"exec,omitempty"`
	Timeout    string   `yaml:"timeout,omitempty" json:"timeout,omitempty"` // exec timeout (default 30s)
	Notify     string   `yaml:"notify,omitempty" json:"notify,omitempty"`   // "webhook"
	Cooldown   string   `yaml:"cooldown,omitempty" json:"cooldown,omitempty"`
	MaxRetries int      `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
}

// WebhookConfig holds webhook endpoint settings.
type WebhookConfig struct {
	URL string `yaml:"url" json:"url"`
}

// AlertsConfig is the top-level YAML structure for self-healing rules.
type AlertsConfig struct {
	Rules   []Rule        `yaml:"rules" json:"rules"`
	Webhook WebhookConfig `yaml:"webhook" json:"webhook"` // legacy, kept for backward compat
	Notify  NotifyConfig  `yaml:"notify" json:"notify"`
}

// CooldownDuration parses the cooldown string into a time.Duration.
// Returns 0 if not set.
func (r *Rule) CooldownDuration() time.Duration {
	if r.Cooldown == "" {
		return 0
	}
	d, err := time.ParseDuration(r.Cooldown)
	if err != nil {
		return 0
	}
	return d
}

// ExecTimeout parses the timeout string into a time.Duration.
// Returns 30s if not set.
func (r *Rule) ExecTimeout() time.Duration {
	if r.Timeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(r.Timeout)
	if err != nil || d <= 0 {
		return 30 * time.Second
	}
	return d
}

// LoadRules reads and parses an alerts YAML config file.
func LoadRules(path string) (*AlertsConfig, error) {
	// Warn if the file has overly permissive permissions (anything beyond 0600).
	if info, err := os.Stat(path); err == nil {
		mode := info.Mode().Perm()
		if mode&0077 != 0 {
			fmt.Fprintf(os.Stderr, "⚠️  alerts config %s has permissions %04o; consider chmod 0600 for security\n", path, mode)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read alerts config: %w", err)
	}

	var cfg AlertsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse alerts config: %w", err)
	}

	if err := validateRules(cfg.Rules); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateRules(rules []Rule) error {
	names := make(map[string]bool, len(rules))
	for _, r := range rules {
		if r.Name == "" {
			return fmt.Errorf("rule is missing a name")
		}
		if names[r.Name] {
			return fmt.Errorf("duplicate rule name: %s", r.Name)
		}
		names[r.Name] = true

		switch r.Metric {
		case "cpu", "memory", "disk":
			if r.Threshold <= 0 || r.Threshold > 100 {
				return fmt.Errorf("rule %q: threshold must be between 1 and 100", r.Name)
			}
		case "container":
			if len(r.Watch) == 0 {
				return fmt.Errorf("rule %q: container metric requires at least one watch target", r.Name)
			}
		default:
			return fmt.Errorf("rule %q: unknown metric %q (must be cpu, memory, disk, or container)", r.Name, r.Metric)
		}

		switch r.Action {
		case "notify", "restart", "exec":
		default:
			return fmt.Errorf("rule %q: unknown action %q (must be notify, restart, or exec)", r.Name, r.Action)
		}

		if r.Action == "exec" && r.Exec == "" {
			return fmt.Errorf("rule %q: exec action requires an exec command", r.Name)
		}
	}
	return nil
}

// DefaultTemplate returns the default alerts.yaml content.
const DefaultTemplate = `rules:
  - name: disk-full
    metric: disk
    threshold: 85
    action: notify
    notify: webhook

  - name: container-down
    metric: container
    watch:
      - nginx-proxy-manager
      - vaultwarden
      - uptime-kuma
    action: restart
    notify: webhook
    cooldown: 5m

  - name: cpu-spike
    metric: cpu
    threshold: 90
    action: notify
    notify: webhook

  - name: memory-high
    metric: memory
    threshold: 85
    action: notify
    notify: webhook

webhook:
  url: ""  # Set your webhook URL (Telegram bot, Slack, Discord, etc.)
`
