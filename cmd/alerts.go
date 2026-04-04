package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/spf13/cobra"
)

func newAlertsCmd() *cobra.Command {
	var watchMode bool
	var interval string
	var alertsConfig string

	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Check resource thresholds (CPU, memory, disk)",
		Long: `Check system resource thresholds for CPU, memory, and disk usage.

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
			result, err := alerts.Check(&cfg.Alerts)
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

	return cmd
}

func newAlertsInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a default alerts.yaml template",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			dir := filepath.Join(home, ".homebutler")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			path := filepath.Join(dir, "alerts.yaml")
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("alerts config already exists: %s\nUse a text editor to modify it", path)
			}
			if err := os.WriteFile(path, []byte(alerts.DefaultTemplate), 0o644); err != nil {
				return fmt.Errorf("failed to write template: %w", err)
			}
			fmt.Printf("Created alerts config: %s\n", path)
			fmt.Println("Edit the file to configure rules and webhook URL.")
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
