package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/service"
	"github.com/Higangssh/homebutler/internal/tui"
	"github.com/Higangssh/homebutler/internal/util"
	"github.com/Higangssh/homebutler/internal/watch"
	isatty "github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Docker restart tracker and TUI dashboard",
		Long: `Track Docker container restarts, capture post-restart logs, and browse history.
Records are stored under ~/.homebutler/watch/.

Subcommands:
  tui        Launch the TUI dashboard
  add        Add a container to the watch list
  list       Show watched containers
  remove     Remove a container from the watch list
  check      Run a one-shot restart check
  start      Start continuous monitoring
  history    List restart history (alias: incidents)
  show       Show details for a specific restart event`,
	}

	watchCmd.AddCommand(
		newWatchTUICmd(),
		newWatchAddCmd(),
		newWatchListCmd(),
		newWatchRemoveCmd(),
		newWatchCheckCmd(),
		newWatchStartCmd(),
		newWatchHistoryCmd(),
		newWatchInstallCmd(),
		newWatchUninstallCmd(),
		newWatchStatusCmd(),
		newWatchShowCmd(),
	)

	return watchCmd
}

func newWatchTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "TUI dashboard (monitors all configured servers)",
		Long:  "Launch the terminal UI dashboard that monitors all configured servers in real-time.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			return tui.Run(cfg, nil)
		},
	}
}

func newWatchAddCmd() *cobra.Command {
	var kind string

	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a container/service to the watch list",
		Long: `Add a Docker container, systemd unit, or PM2 app to the watch list.
If no name is given and kind is docker, lists running containers for reference.
When a name is given without --kind, an interactive prompt lets you choose the type.

Examples:
  homebutler watch add nginx                      # interactive type selection
  homebutler watch add --kind docker nginx        # non-interactive
  homebutler watch add --kind systemd nginx.service
  homebutler watch add --kind pm2 my-api`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kindChanged := cmd.Flags().Changed("kind")

			// If --kind was explicitly provided, validate it
			if kindChanged {
				switch kind {
				case "docker", "systemd", "pm2":
				default:
					return fmt.Errorf("invalid kind %q: must be docker, systemd, or pm2", kind)
				}
			}

			if len(args) == 0 {
				if !kindChanged {
					kind = "docker"
				}
				if kind == "docker" {
					containers, err := docker.List()
					if err != nil {
						return fmt.Errorf("cannot list containers: %w", err)
					}
					if len(containers) == 0 {
						fmt.Println("No running containers found.")
						return nil
					}
					fmt.Println("Running containers:")
					for _, c := range containers {
						if c.State == "running" {
							fmt.Printf("  %s  (%s)\n", c.Name, c.Image)
						}
					}
					fmt.Println("\nUsage: homebutler watch add <container-name>")
				} else {
					fmt.Printf("Usage: homebutler watch add --kind %s <name>\n", kind)
				}
				return nil
			}

			name := args[0]

			// If --kind was not explicitly given, prompt interactively
			if !kindChanged {
				if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
					return fmt.Errorf("--kind flag is required when stdin is not a terminal")
				}
				options := []string{"docker", "systemd", "pm2"}
				labels := []string{"Docker container", "systemd service", "PM2 application"}
				idx, err := promptSelect(bufio.NewScanner(os.Stdin), "Select process type", options, labels)
				if err != nil {
					return err
				}
				kind = options[idx]
			}
			if !isValidTargetName(name) {
				return fmt.Errorf("invalid name: %s", name)
			}

			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}

			targets, err := watch.LoadTargets(dir)
			if err != nil {
				return err
			}

			for _, t := range targets {
				if t.Container == name && t.EffectiveKind() == kind {
					fmt.Printf("%s %q is already being watched.\n", kind, name)
					return nil
				}
			}

			targets = append(targets, watch.Target{
				Container: name,
				Kind:      kind,
				Unit:      name,
				AddedAt:   time.Now(),
			})
			if err := watch.SaveTargets(dir, targets); err != nil {
				return err
			}

			// Seed initial state for docker targets
			if kind == "docker" {
				result, inspErr := watch.InspectContainer(name)
				if inspErr == nil {
					states, _ := watch.LoadState(dir)
					if states == nil {
						states = make(map[string]*watch.ContainerState)
					}
					states[name] = &watch.ContainerState{
						Container:    name,
						RestartCount: result.RestartCount,
						StartedAt:    result.StartedAt,
						LastChecked:  time.Now(),
					}
					_ = watch.SaveState(dir, states)
				}
			}

			fmt.Printf("Added %s %q to watch list.\n", kind, name)
			if cmdline := service.RestartNote(); cmdline != "" {
				fmt.Printf("  The running service reads the watch list at startup:\n    %s\n", cmdline)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "docker", "Target kind: docker, systemd, or pm2")
	return cmd
}

func newWatchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "Show watched containers",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}
			watched, err := watch.ListWatched(dir)
			if err != nil {
				return err
			}
			if jsonOutput {
				return output(watched, true)
			}
			if len(watched) == 0 {
				fmt.Println("No targets being watched. Use 'homebutler watch add <name>' to add one.")
				return nil
			}

			fmt.Printf("%-25s %-10s %-22s %-10s %s\n", "NAME", "KIND", "ADDED", "RESTARTS", "LAST CHECKED")
			for _, w := range watched {
				restarts := "-"
				lastChecked := "-"
				if w.LastChecked != "" {
					restarts = fmt.Sprintf("%d", w.RestartCount)
					lastChecked = w.LastChecked
				}
				fmt.Printf("%-25s %-10s %-22s %-10s %s\n", w.Container, w.Kind, w.AddedAt, restarts, lastChecked)
			}
			return nil
		},
	}
}

func newWatchRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <container>",
		Short: "Remove a container from the watch list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}
			targets, err := watch.LoadTargets(dir)
			if err != nil {
				return err
			}

			found := false
			var filtered []watch.Target
			for _, t := range targets {
				if t.Container == name {
					found = true
					continue
				}
				filtered = append(filtered, t)
			}
			if !found {
				return fmt.Errorf("container %q is not in the watch list", name)
			}

			if err := watch.SaveTargets(dir, filtered); err != nil {
				return err
			}
			fmt.Printf("Removed %q from watch list.\n", name)
			if cmdline := service.RestartNote(); cmdline != "" {
				fmt.Printf("  The running service reads the watch list at startup:\n    %s\n", cmdline)
			}
			return nil
		},
	}
}

func newWatchCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Run a one-shot restart check on all watched containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Loaded so config.yaml's watch settings reach this command too;
			// without it check and start would disagree about retention.
			if err := loadConfig(); err != nil {
				return err
			}
			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}
			targets, err := watch.LoadTargets(dir)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				fmt.Println("No targets being watched.")
				return nil
			}

			result, err := watch.CheckTargets(dir, resolveIncidentCap(dir))
			if err != nil {
				return err
			}

			if jsonOutput {
				return output(result, true)
			}

			fmt.Printf("Checked %d docker target(s).\n", result.Checked)
			if len(result.Incidents) == 0 {
				fmt.Println("No restarts detected.")
			} else {
				fmt.Printf("%d restart(s) detected:\n", len(result.Incidents))
				for _, inc := range result.Incidents {
					fmt.Printf("  %s: %s (%s)\n", inc.Container, inc.ID, restartLabel(inc.RestartCount))
				}
			}

			// Without this, a watch list of only systemd/pm2 targets prints
			// "No restarts detected" without having looked at anything.
			if len(result.Skipped) > 0 {
				fmt.Printf("\n⚠️  Skipped %d target(s) that check cannot inspect:\n", len(result.Skipped))
				for _, s := range result.Skipped {
					fmt.Printf("  %s (%s)\n", s.Name, s.Kind)
				}
				fmt.Println("  → These are only monitored while running: homebutler watch start")
			}
			return nil
		},
	}
}

func newWatchStartCmd() *cobra.Command {
	var interval string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start continuous monitoring",
		Long: `Start foreground monitoring using event-based or polling monitors.
Docker targets use docker events (real-time). Systemd and PM2 targets use polling.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Other commands load the global config here; this one didn't,
			// so config.yaml's notify settings were never reachable below.
			if err := loadConfig(); err != nil {
				return err
			}

			dur, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid interval %q: %w", interval, err)
			}
			if dur < 5*time.Second {
				return fmt.Errorf("interval must be at least 5s")
			}

			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}

			targets, err := watch.LoadTargets(dir)
			if err != nil {
				return err
			}
			// An empty watch list is not a reason to stop. Since #97 this process
			// also checks resource thresholds and runs remediation rules, and
			// neither needs a target. Exiting here made a supervisor restart the
			// process every ThrottleInterval for as long as the list stayed
			// empty — a loop found by installing it rather than by any test.
			if len(targets) == 0 {
				fmt.Println("Nothing on the watch list; monitoring thresholds only.")
				fmt.Println("  Add something with: homebutler watch add <name>")
			}

			// Load watch config (config.yaml preferred, watch/config.json fallback)
			watchCfg, cfgErr := watch.LoadWatchConfig(dir)
			if cfgErr != nil {
				fmt.Fprintf(os.Stderr, "warning: cannot load watch config: %v, using defaults\n", cfgErr)
				defaultCfg := watch.DefaultWatchConfig()
				watchCfg = &defaultCfg
			}
			if cfg != nil {
				watchCfg.Notify = cfg.Watch.Notify
				watchCfg.Flapping = cfg.Watch.Flapping
				watchCfg.Retention = cfg.Watch.Retention
			}
			watchCfg.Retention.Normalize()

			// One read serves both the notification providers and the rules
			// engine. loadAlertsConfig already prefers rules declared in
			// config.yaml and only falls back to the deprecated
			// ~/.homebutler/alerts.yaml, so reading it a second time would
			// print its deprecation and permission warnings twice.
			//
			// A configuration that will not load is reported and skipped. The
			// restart monitors are why this process exists and they should not
			// fail to start because remediation rules do not parse.
			alertsCfg, alertsErr := loadAlertsConfig("")
			if alertsErr != nil {
				fmt.Fprintf(os.Stderr, "warning: cannot load alerts configuration: %v; continuing without thresholds or rules\n", alertsErr)
			}

			providers := &alerts.NotifyConfig{}
			if cfg != nil {
				providers = &cfg.Notify
			}
			if providers.IsEmpty() && alertsCfg != nil {
				providers = alerts.ResolveNotifyConfig(alertsCfg)
			}

			var notifier *watch.WatchNotifier
			if watchCfg.Notify.Enabled {
				notifier = watch.NewWatchNotifier(watchCfg.Notify, providers)
			}

			// Group targets by kind
			var dockerTargets, systemdTargets, pm2Targets []watch.Target
			for _, t := range targets {
				switch t.EffectiveKind() {
				case "systemd":
					systemdTargets = append(systemdTargets, t)
				case "pm2":
					pm2Targets = append(pm2Targets, t)
				default:
					dockerTargets = append(dockerTargets, t)
				}
			}

			fmt.Printf("Starting monitors (polling interval=%s). Press Ctrl+C to stop.\n", dur)
			fmt.Printf("  Docker targets: %d, Systemd targets: %d, PM2 targets: %d\n",
				len(dockerTargets), len(systemdTargets), len(pm2Targets))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

			// Resource thresholds and, when configured, the rules engine run in
			// this same process. Polling a level is a different detection model
			// from reacting to a restart, but it is the same job from the
			// operator's side, and splitting it across two processes is what
			// #97 is about. The rules engine matters most here: it holds the
			// only remediation path there is, and it could not previously see a
			// restart incident because it ran somewhere else.
			// Bound the file this process's own output goes into. Only launchd
			// needs it: journald rotates what systemd collects, and macOS
			// rotates nothing without a newsyslog.d entry that needs root.
			var logTick <-chan time.Time
			var logPath string
			if kind, err := service.Detect(); err == nil {
				if home, err := os.UserHomeDir(); err == nil {
					if lp := service.LogPath(kind, home); lp != "" {
						logPath = lp
						_ = service.TrimLog(logPath, service.MaxLogBytes)
						ticker := time.NewTicker(time.Hour)
						defer ticker.Stop()
						logTick = ticker.C
					}
				}
			}

			thresholdCh := alerts.Watch(ctx, alerts.WatchConfig{Interval: dur, Alert: cfg.Alerts})

			var ruleCh <-chan string
			if alertsCfg != nil && len(alertsCfg.Rules) > 0 {
				warnUnprivilegedSystemdRules(alertsCfg)
				var flap alerts.FlapChecker = watch.IncidentHistory{Dir: dir, Flapping: watchCfg.Flapping}
				ruleCh = alerts.WatchRules(ctx, dur, alertsCfg, flap)
				fmt.Printf("  Rules: %d loaded, remediation active\n", len(alertsCfg.Rules))
			}

			incCh := make(chan watch.Incident, 64)

			// Generic command runner for systemd/pm2 (non-docker)
			genericRunner := func(name string, args ...string) (string, error) {
				return util.RunCmd(name, args...)
			}

			// Launch monitors with WaitGroup to track goroutine completion
			var wg sync.WaitGroup
			monitorCount := 0

			if len(dockerTargets) > 0 {
				monitorCount++
				wg.Add(1)
				dm := &watch.DockerMonitor{Dir: dir, PostLogDelay: 5 * time.Second}
				go func() {
					defer wg.Done()
					if err := dm.Watch(ctx, dockerTargets, incCh); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "[docker-monitor] error: %v\n", err)
					}
				}()
			}

			if len(systemdTargets) > 0 {
				monitorCount++
				wg.Add(1)
				sm := &watch.SystemdMonitor{Run: genericRunner, Dir: dir, Interval: dur}
				go func() {
					defer wg.Done()
					if err := sm.Watch(ctx, systemdTargets, incCh); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "[systemd-monitor] error: %v\n", err)
					}
				}()
			}

			if len(pm2Targets) > 0 {
				monitorCount++
				wg.Add(1)
				pm := &watch.PM2Monitor{Run: genericRunner, Dir: dir, Interval: dur}
				go func() {
					defer wg.Done()
					if err := pm.Watch(ctx, pm2Targets, incCh); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "[pm2-monitor] error: %v\n", err)
					}
				}()
			}

			proxmoxTargets := buildProxmoxTargets()
			if len(proxmoxTargets) > 0 {
				monitorCount++
				wg.Add(1)
				proxmoxMon := &watch.ProxmoxMonitor{Targets: proxmoxTargets, Dir: dir, Interval: dur, Keep: watchCfg.Retention.MaxIncidents, Flapping: watchCfg.Flapping}
				go func() {
					defer wg.Done()
					if err := proxmoxMon.Watch(ctx, incCh); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "[proxmox-monitor] error: %v\n", err)
					}
				}()
				fmt.Printf("  Proxmox endpoints: %d\n", len(proxmoxTargets))
			}

			// With no restart monitors there is nothing to close incCh, and a
			// nil channel is never selected — so the loop below waits on the
			// thresholds and the signal instead of seeing a closed channel and
			// treating it as "all monitors stopped".
			var incidents <-chan watch.Incident
			if monitorCount == 0 {
				fmt.Println("  No restart monitors to start.")
			} else {
				incidents = incCh
			}

			// Close incCh when all monitors are done
			go func() {
				wg.Wait()
				close(incCh)
			}()

			// Print incidents as they arrive
			for {
				select {
				case inc, ok := <-incidents:
					if !ok {
						fmt.Println("\nAll monitors stopped.")
						return nil
					}

					// Proxmox incidents are state transitions on an endpoint or a
					// configured guest, not a process restart: they carry no exit
					// code or logs to analyze, and "flapping" would only measure
					// how often a poll caught a different answer. The monitor has
					// already saved the incident itself.
					if inc.Source == "proxmox" {
						if notifier != nil {
							flap := watch.FlappingResult{}
							if inc.Flapping != nil {
								flap = *inc.Flapping
							}
							_ = notifier.NotifyIncident(inc, flap, nil, time.Now())
						}
						ts := time.Now().Format("15:04:05")
						verb := "INCIDENT"
						if inc.Recovered {
							verb = "RECOVERED"
						}
						fmt.Printf("[%s] PROXMOX %s: %s\n", ts, verb, inc.PostLogs)
						continue
					}

					// The exit code is the analyser's highest-confidence signal —
					// 137 is OOM, 139 a segfault, 143 a SIGTERM — and leaving it
					// unset meant every incident reached `case 0` and was
					// reported as a clean exit (#108).
					crashInfo := watch.CrashInfo{
						ErrorLog:  inc.PreLogs,
						Backend:   getBackendKind(inc.Container, targets),
						OOMKilled: inc.OOMKilled,
					}
					if inc.ExitCode != nil {
						crashInfo.ExitCode = *inc.ExitCode
					}
					summary := watch.Analyze(crashInfo)
					inc.CrashAnalysis = &summary

					// Refs, not incidents: this runs on every save, and the
					// decision only needs the container and the time, both of
					// which the filename already carries.
					refs, _ := watch.ListIncidentRefs(dir)
					flapResult := watchCfg.Flapping.CheckRefs(inc.Container, refs, time.Now())
					if flapResult.IsFlapping {
						inc.Flapping = &flapResult
					}

					_ = watch.SaveIncident(dir, &inc, watchCfg.Retention.MaxIncidents)

					if notifier != nil {
						_ = notifier.NotifyIncident(inc, flapResult, &summary, time.Now())
					}

					ts := time.Now().Format("15:04:05")
					fmt.Printf("[%s] INCIDENT: %s (incident %s)\n", ts, inc.Container, inc.ID)
					fmt.Printf("  Crash: %s (%s, confidence: %s)\n", summary.Reason, summary.Category, summary.Confidence)
					if flapResult.IsFlapping {
						fmt.Printf("  ⚠ FLAPPING: %s (%d restarts in %s window)\n", flapResult.Level, flapResult.Count, flapResult.Window)
					}
				case e, ok := <-thresholdCh:
					if !ok {
						thresholdCh = nil
						continue
					}
					fmt.Println(alerts.FormatEvent(e))
					if providers != nil && !providers.IsEmpty() {
						_ = alerts.NotifyAll(providers, alerts.NotifyEvent{
							RuleName: e.Resource,
							Status:   e.Severity,
							Details:  e.Message,
							Time:     e.Time.Format(time.RFC3339),
						})
					}
				case msg, ok := <-ruleCh:
					if !ok {
						ruleCh = nil
						continue
					}
					fmt.Println(msg)
				case <-logTick:
					if err := service.TrimLog(logPath, service.MaxLogBytes); err != nil {
						fmt.Fprintf(os.Stderr, "warning: trim %s: %v\n", logPath, err)
					}
				case <-sig:
					fmt.Println("\nStopping all monitors.")
					cancel()
					return nil
				}
			}
		},
	}

	cmd.Flags().StringVar(&interval, "interval", "30s", "Check/poll interval (e.g. 30s, 1m, 5m)")
	return cmd
}

// resolveIncidentCap resolves the incident retention cap the way watch start
// does: config.yaml wins over watch/config.json, and an unset value takes the
// default rather than reading as unlimited.
func resolveIncidentCap(dir string) int {
	watchCfg, err := watch.LoadWatchConfig(dir)
	if err != nil || watchCfg == nil {
		defaults := watch.DefaultWatchConfig()
		watchCfg = &defaults
	}
	if cfg != nil {
		watchCfg.Retention = cfg.Watch.Retention
	}
	watchCfg.Retention.Normalize()
	return watchCfg.Retention.MaxIncidents
}

func newWatchHistoryCmd() *cobra.Command {
	var (
		limit     int
		container string
		logs      bool
	)
	cmd := &cobra.Command{
		Use:     "history",
		Aliases: []string{"incidents"},
		Short:   "List restart history",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}
			incidents, err := watch.History(dir, watch.HistoryOptions{
				Limit:     limit,
				Container: container,
				Logs:      logs,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return output(incidents, true)
			}
			if len(incidents) == 0 {
				fmt.Println("No restart history recorded.")
				return nil
			}

			fmt.Printf("%-20s  %-36s  %-20s  %s\n", "CONTAINER", "INCIDENT ID", "DETECTED", "INFO")
			for _, inc := range incidents {
				id := inc.ID
				if inc.RestartCount > 0 {
					id = fmt.Sprintf("%s (restart #%d)", inc.ID, inc.RestartCount)
				}
				info := ""
				if inc.Flapping != nil {
					info += "[FLAPPING] "
				}
				if inc.CrashAnalysis != nil {
					info += inc.CrashAnalysis.Category
				}
				if inc.Source == "proxmox" {
					if inc.Recovered {
						info += "[RECOVERED] "
					}
					info += inc.ProxmoxState + " " + inc.PostLogs
				}
				fmt.Printf("%-20s  %-36s  %-20s  %s\n",
					inc.Container, id,
					inc.DetectedAt.Format("2006-01-02 15:04:05"),
					info)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Show only the most recent N incidents (0 = all)")
	cmd.Flags().StringVar(&container, "container", "", "Show only incidents for this target")
	// Only affects --json. The table never printed logs, and a hundred lines of
	// container output twice per incident is not something to put in a table.
	cmd.Flags().BoolVar(&logs, "logs", false, "Include captured logs in --json output")
	return cmd
}

func newWatchShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <incident-id>",
		Short: "Show incident details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := watch.WatchDir()
			if err != nil {
				return err
			}
			inc, err := watch.LoadIncident(dir, args[0])
			if err != nil {
				return err
			}
			if jsonOutput {
				return output(inc, true)
			}

			fmt.Printf("Incident:  %s\n", inc.ID)
			fmt.Printf("Container: %s\n", inc.Container)
			fmt.Printf("Detected:  %s\n", inc.DetectedAt.Format("2006-01-02 15:04:05"))
			if inc.RestartCount > 0 {
				fmt.Printf("Restarts:  %d\n", inc.RestartCount)
			}
			if inc.Source == "proxmox" {
				fmt.Printf("State:     %s\n", inc.ProxmoxState)
				if inc.ProxmoxClass != "" {
					fmt.Printf("Class:     %s\n", inc.ProxmoxClass)
				}
				fmt.Printf("Recovered: %t\n", inc.Recovered)
			}
			fmt.Printf("Previous Start: %s\n", inc.PrevStarted)
			fmt.Printf("Current Start:  %s\n", inc.CurrStarted)
			if inc.CrashAnalysis != nil {
				fmt.Println()
				fmt.Println("=== Crash Analysis ===")
				fmt.Printf("Category:   %s\n", inc.CrashAnalysis.Category)
				fmt.Printf("Reason:     %s\n", inc.CrashAnalysis.Reason)
				fmt.Printf("Confidence: %s\n", inc.CrashAnalysis.Confidence)
				if inc.CrashAnalysis.Signal != "" {
					fmt.Printf("Signal:     %s\n", inc.CrashAnalysis.Signal)
				}
				if len(inc.CrashAnalysis.Patterns) > 0 {
					fmt.Printf("Patterns:   %s\n", strings.Join(inc.CrashAnalysis.Patterns, ", "))
				}
			}
			if inc.Flapping != nil {
				fmt.Println()
				fmt.Printf("⚠ FLAPPING: %s (%d restarts in %s window, since %s)\n",
					inc.Flapping.Level, inc.Flapping.Count, inc.Flapping.Window,
					inc.Flapping.Since.Format("15:04:05"))
			}
			fmt.Println()
			if inc.PreLogs != "" {
				fmt.Println("=== Pre-Death Logs ===")
				preLines := inc.PreLogs
				preRunes := []rune(preLines)
				if len(preRunes) > 5000 {
					preLines = string(preRunes[:5000]) + "\n... (truncated)"
				}
				fmt.Println(preLines)
				fmt.Println()
			}
			fmt.Println("=== Post-Restart Logs ===")
			logLines := inc.PostLogs
			runes := []rune(logLines)
			if len(runes) > 5000 {
				logLines = string(runes[:5000]) + "\n... (truncated)"
			}
			fmt.Println(logLines)
			return nil
		},
	}
}

func getBackendKind(container string, targets []watch.Target) string {
	for _, t := range targets {
		if t.Container == container || t.EffectiveUnit() == container {
			return t.EffectiveKind()
		}
	}
	return "docker"
}

func restartLabel(count int) string {
	if count > 0 {
		return fmt.Sprintf("restart #%d", count)
	}
	return "restart detected"
}

// promptSelect displays a numbered list of options and returns the chosen index.
// labels are the display strings; options are the underlying values (used only for caller).
func promptSelect(scanner *bufio.Scanner, prompt string, options, labels []string) (int, error) {
	fmt.Printf("? %s:\n", prompt)
	for i, label := range labels {
		fmt.Printf("  %d) %s\n", i+1, label)
	}
	for {
		fmt.Print("Enter number [1]: ")
		if !scanner.Scan() {
			return 0, fmt.Errorf("interrupted")
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(options) {
			fmt.Printf("Please enter a number between 1 and %d.\n", len(options))
			continue
		}
		return n - 1, nil
	}
}

func isValidTargetName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
