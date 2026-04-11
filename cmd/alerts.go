package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newAlertsCmd() *cobra.Command {
	var watchMode bool
	var interval string
	var alertsConfig string

	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Advanced resource threshold checks (CPU, memory, disk)",
		Long: `Check system resource thresholds for CPU, memory, and disk usage.

This is an advanced threshold-based workflow. For most users, start with
'homebutler watch' for restart detection, crash analysis, flapping detection,
and incident history.

Use --watch to continuously monitor resources (Ctrl+C to stop).
Use --interval to set the monitoring interval (default: 30s).
Use --config to load YAML rules for self-healing mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			if watchMode {
				return runAlertsWatch(interval, alertsConfig)
			}
			result, err := alerts.Check(&config.AlertConfig{CPU: cfg.Alerts.CPU, Memory: cfg.Alerts.Memory, Disk: cfg.Alerts.Disk})
			if err != nil {
				return fmt.Errorf("failed to check alerts: %w", err)
			}
			return output(result, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&watchMode, "watch", false, "Continuously monitor resources (Ctrl+C to stop)")
	cmd.Flags().StringVar(&interval, "interval", "30s", "Monitoring interval (e.g. 30s, 1m)")
	cmd.Flags().StringVar(&alertsConfig, "alerts-config", "", "Path to alerts YAML config for self-healing rules")

	cmd.AddCommand(newAlertsInitCmd())
	cmd.AddCommand(newAlertsHistoryCmd())
	cmd.AddCommand(newAlertsTestNotifyCmd())

	return cmd
}

func newAlertsInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactively generate a user-friendly config.yaml template",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			path := filepath.Join(home, ".config", "homebutler", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			// If file exists, ask whether to overwrite
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("%s already exists. Overwrite? [y/N]: ", path)
				scanner := bufio.NewScanner(os.Stdin)
				scanner.Scan()
				ans := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if ans != "y" && ans != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			result, err := alerts.RunInitPrompt(os.Stdin, os.Stdout, docker.List)
			if err != nil {
				return err
			}

			yamlStr, err := alerts.BuildYAML(result)
			if err != nil {
				return err
			}

			if err := os.WriteFile(path, []byte(yamlStr), 0o644); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
			fmt.Println()
			fmt.Println(okStyle.Render(fmt.Sprintf("✅ Created %s", path)))
			fmt.Println(okStyle.Render("🛡️  Run: homebutler alerts --watch"))
			fmt.Println("Legacy ~/.homebutler/alerts.yaml is still supported as fallback, but config.yaml is now preferred.")
			return nil
		},
	}
}

func newAlertsHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "Show recent alert and remediation history",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := alerts.LoadHistory()
			if err != nil {
				return fmt.Errorf("failed to load history: %w", err)
			}
			if jsonOutput {
				return output(entries, true)
			}
			fmt.Print(alerts.FormatHistory(entries))
			return nil
		},
	}
}

func newAlertsTestNotifyCmd() *cobra.Command {
	var alertsConfig string

	cmd := &cobra.Command{
		Use:   "test-notify",
		Short: "Send a test notification to all configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			rulesCfg, err := loadAlertsConfig(alertsConfig)
			if err != nil {
				return err
			}
			if rulesCfg == nil {
				return fmt.Errorf("no notification config found; configure notify in config.yaml or use --alerts-config for legacy alerts.yaml")
			}

			notifyCfg := alerts.ResolveNotifyConfig(rulesCfg)
			if notifyCfg == nil {
				return fmt.Errorf("no notification providers configured")
			}

			event := alerts.NotifyEvent{
				RuleName: "test-notification",
				Status:   "triggered",
				Details:  "This is a test notification from homebutler",
				Action:   "notify",
				Result:   "success",
				Time:     time.Now().Format("2006-01-02 15:04:05"),
			}

			fmt.Println("Sending test notification...")
			errs := alerts.NotifyAll(notifyCfg, event)

			// Report results per provider
			providers := []string{}
			if notifyCfg.Telegram != nil {
				providers = append(providers, "telegram")
			}
			if notifyCfg.Slack != nil {
				providers = append(providers, "slack")
			}
			if notifyCfg.Discord != nil {
				providers = append(providers, "discord")
			}
			if notifyCfg.Webhook != nil {
				providers = append(providers, "webhook")
			}

			errMap := make(map[string]bool)
			for _, e := range errs {
				for _, p := range providers {
					if strings.Contains(e.Error(), p) {
						errMap[p] = true
					}
				}
			}

			for _, p := range providers {
				if errMap[p] {
					fmt.Printf("  ❌ %s: failed\n", p)
				} else {
					fmt.Printf("  ✅ %s: sent\n", p)
				}
			}

			if len(errs) > 0 {
				return fmt.Errorf("%d provider(s) failed", len(errs))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&alertsConfig, "alerts-config", "", "Path to alerts YAML config")
	return cmd
}

func loadAlertsConfig(alertsConfigPath string) (*alerts.AlertsConfig, error) {
	if cfg != nil {
		uc := &alerts.UserConfig{}
		uc.Notify = cfg.Notify
		uc.Watch.Enabled = cfg.Watch.Notify.Enabled
		uc.Watch.NotifyOn = cfg.Watch.Notify.NotifyOn
		uc.Watch.Cooldown = cfg.Watch.Notify.Cooldown
		uc.Alerts.CPU = cfg.Alerts.CPU
		uc.Alerts.Memory = cfg.Alerts.Memory
		uc.Alerts.Disk = cfg.Alerts.Disk
		data, err := os.ReadFile(cfg.Path)
		if err == nil {
			_ = yaml.Unmarshal(data, uc)
		}
		if len(uc.Alerts.Rules) > 0 || !uc.Notify.IsEmpty() {
			return alerts.FromConfigRules(uc.Alerts.Rules, uc.Notify)
		}
	}
	if alertsConfigPath != "" {
		return alerts.LoadRules(alertsConfigPath)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	defaultPath := filepath.Join(home, ".homebutler", "alerts.yaml")
	if _, statErr := os.Stat(defaultPath); statErr != nil {
		return nil, nil
	}
	fmt.Fprintln(os.Stderr, "warning: ~/.homebutler/alerts.yaml is deprecated, move rules/notify into config.yaml")
	return alerts.LoadRules(defaultPath)
}

func runAlertsWatch(intervalStr, alertsConfigPath string) error {
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", intervalStr, err)
	}

	// Try to load YAML rules for self-healing
	var rulesCfg *alerts.AlertsConfig
	if alertsConfigPath != "" {
		rulesCfg, err = alerts.LoadRules(alertsConfigPath)
		if err != nil {
			return fmt.Errorf("failed to load alerts config: %w", err)
		}
	} else {
		// Try default path
		home, _ := os.UserHomeDir()
		if home != "" {
			defaultPath := filepath.Join(home, ".homebutler", "alerts.yaml")
			if _, statErr := os.Stat(defaultPath); statErr == nil {
				rulesCfg, err = alerts.LoadRules(defaultPath)
				if err != nil {
					return fmt.Errorf("failed to load alerts config: %w", err)
				}
			}
		}
	}

	// If we have rules, run self-healing watch
	if rulesCfg != nil && len(rulesCfg.Rules) > 0 {
		return runSelfHealingWatch(interval, rulesCfg)
	}

	// Fallback to basic watch mode
	watchCfg := alerts.WatchConfig{
		Interval: interval,
		Alert:    cfg.Alerts,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "🔍 Watching local server (interval: %s, Ctrl+C to stop)\n\n", interval)

	events := alerts.Watch(ctx, watchCfg)
	for e := range events {
		fmt.Println(alerts.FormatEvent(e))
	}
	fmt.Fprintln(os.Stderr, "\n👋 Stopped watching.")
	return nil
}

func runSelfHealingWatch(interval time.Duration, rulesCfg *alerts.AlertsConfig) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "🛡️ Self-Healing active — watching %d rules (interval: %s, Ctrl+C to stop)\n\n",
		len(rulesCfg.Rules), interval)

	events := alerts.WatchRules(ctx, interval, rulesCfg)
	for e := range events {
		fmt.Println(e)
	}
	fmt.Fprintln(os.Stderr, "\n👋 Stopped watching.")
	return nil
}
