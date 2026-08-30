package cmd

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/service"
	"github.com/Higangssh/homebutler/internal/watch"
	"github.com/spf13/cobra"
)

func newWatchInstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register watch with the host's service supervisor",
		Long: `Write a service unit so monitoring survives logout and reboot.

homebutler does not run a daemon of its own. This hands the monitoring loop to
whatever the host already supervises with: a systemd user unit on Linux, a
launchd agent on macOS.

Both are user-level, and neither is a preference. On Linux the watch list lives
under the invoking user's home directory, so a unit running as root would find
an empty list and monitor nothing. On macOS Docker Desktop only runs inside a
logged-in session, so a LaunchDaemon would poll a daemon that is not there.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := service.Detect()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot determine the homebutler binary path: %w", err)
			}

			path := service.UnitPath(kind, home)
			if service.Installed(path) && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
			}
			if err := service.Write(path, service.Render(kind, exe, home)); err != nil {
				return err
			}

			start := service.StartCommand(kind, path)
			if err := service.Run(start); err != nil {
				// The unit is written; only activation failed. Say both, so the
				// operator knows what to remove and what to run by hand.
				return fmt.Errorf("wrote %s but could not start it: %w", path, err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "✅ %s unit written to %s\n", kind, path)
			fmt.Fprintf(out, "✅ enabled and started\n")
			if note := service.LingerNote(kind); note != "" {
				fmt.Fprintf(out, "\n⚠️  %s\n", note)
			}
			if dir, err := watch.WatchDir(); err == nil {
				if targets, err := watch.LoadTargets(dir); err == nil && len(targets) == 0 {
					fmt.Fprintf(out, "\nNothing is on the watch list yet — it will monitor nothing until you add something:\n    homebutler watch add <name>\n")
				}
			}
			fmt.Fprintf(out, "\nUndo with: homebutler watch uninstall\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing unit")
	return cmd
}

func newWatchUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the service unit installed by watch install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := service.Detect()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			path := service.UnitPath(kind, home)
			if !service.Installed(path) {
				return fmt.Errorf("no unit installed at %s", path)
			}

			// Stopping can fail on a unit that is already stopped, which is not
			// a reason to leave the file behind.
			stopErr := service.Run(service.StopCommand(kind, path))
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove %s: %w", path, err)
			}

			out := cmd.OutOrStdout()
			if stopErr != nil {
				fmt.Fprintf(out, "⚠️  could not stop it cleanly: %v\n", stopErr)
			}
			fmt.Fprintf(out, "✅ removed %s\n", path)
			return nil
		},
	}
}

func newWatchStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "installed",
		Short: "Report whether watch is registered with the supervisor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := service.Detect()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			path := service.UnitPath(kind, home)
			plan := service.Plan{Kind: kind, Path: path, Start: service.StartCommand(kind, path)}
			if jsonOutput {
				return output(map[string]any{"installed": service.Installed(path), "plan": plan}, true)
			}
			out := cmd.OutOrStdout()
			if service.Installed(path) {
				fmt.Fprintf(out, "Installed: %s (%s)\n", path, kind)
				return nil
			}
			fmt.Fprintf(out, "Not installed. %s would be written to %s\n", kind, path)
			fmt.Fprintf(out, "    homebutler watch install\n")
			return nil
		},
	}
}
