package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/spf13/cobra"
)

func newAlertsCmd() *cobra.Command {
	var watchMode bool
	var interval string

	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Check resource thresholds (CPU, memory, disk)",
		Long: `Check system resource thresholds for CPU, memory, and disk usage.

Use --watch to continuously monitor resources (Ctrl+C to stop).
Use --interval to set the monitoring interval (default: 30s).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			if watchMode {
				return runAlertsWatch(interval)
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

	return cmd
}

func runAlertsWatch(intervalStr string) error {
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", intervalStr, err)
	}

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
