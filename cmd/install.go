package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "install <app> [options]",
		Short: "Install and manage homelab apps",
		Long: `Install and manage homelab apps via Docker Compose.

Usage:
  homebutler install <app>                Install an app
  homebutler install <app> --port 8080    Custom port
  homebutler install list                 List available apps
  homebutler install status <app>         Check app status
  homebutler install uninstall <app>      Stop (keep data)
  homebutler install purge <app>          Stop + delete data`,
		Args:                  cobra.ArbitraryArgs,
		DisableFlagParsing:    false,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			// This handles the case where someone types "homebutler install <appname>"
			// directly (not via a subcommand).
			return runInstallApp(args[0], cmd)
		},
	}

	installCmd.Flags().String("port", "", "Custom host port")
	installCmd.Flags().String("media", "", "Media directory to mount (jellyfin)")

	installCmd.AddCommand(
		newInstallListCmd(),
		newInstallStatusCmd(),
		newInstallUninstallCmd(),
		newInstallPurgeCmd(),
	)

	return installCmd
}

func newInstallListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List available apps",
		RunE: func(cmd *cobra.Command, args []string) error {
			apps := install.List()
			jsonOut, _ := cmd.Root().PersistentFlags().GetBool("json")
			if jsonOut {
				return output(apps, true)
			}
			fmt.Fprintf(os.Stderr, "📦 Available apps (%d):\n\n", len(apps))
			for _, app := range apps {
				fmt.Fprintf(os.Stderr, "  %-20s %s\n", app.Name, app.Description)
			}
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "Usage: homebutler install <app>")
			return nil
		},
	}
}

func newInstallStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <app>",
		Short: "Check app status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := install.Status(args[0])
			if err != nil {
				return err
			}
			icon := "🔴"
			if status == "running" {
				icon = "🟢"
			}
			fmt.Fprintf(os.Stderr, "%s %s: %s\n", icon, args[0], status)
			return nil
		},
	}
}

func newInstallUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "uninstall <app>",
		Aliases: []string{"rm"},
		Short:   "Stop an app (keep data)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]
			fmt.Fprintf(os.Stderr, "🛑 Stopping %s...\n", appName)
			if err := install.Uninstall(appName); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "✅ Stopped and removed containers")
			fmt.Fprintf(os.Stderr, "💡 Data preserved at: %s\n", install.GetInstalledPath(appName))
			fmt.Fprintf(os.Stderr, "   To delete everything: homebutler install purge %s\n", appName)
			return nil
		},
	}
}

func newInstallPurgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "purge <app>",
		Short: "Stop an app and delete all data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]
			fmt.Fprintf(os.Stderr, "⚠️  Purging %s (containers + data)...\n", appName)
			if err := install.Purge(appName); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "✅ Completely removed")
			return nil
		},
	}
}

func runInstallApp(appName string, cmd *cobra.Command) error {
	app, ok := install.Registry[appName]
	if !ok {
		available := make([]string, 0)
		for name := range install.Registry {
			available = append(available, name)
		}
		return fmt.Errorf("unknown app %q. Available: %s", appName, strings.Join(available, ", "))
	}

	// Parse flags from the parent command
	portFlag, _ := cmd.Flags().GetString("port")
	mediaFlag, _ := cmd.Flags().GetString("media")
	opts := install.InstallOptions{
		Port:     portFlag,
		MediaDir: mediaFlag,
	}

	port := app.DefaultPort
	if opts.Port != "" {
		port = opts.Port
	}

	// Pre-check
	fmt.Fprintf(os.Stderr, "🔍 Checking prerequisites for %s...\n", app.Name)
	issues := install.PreCheck(app, port)
	if len(issues) > 0 {
		fmt.Fprintln(os.Stderr, "❌ Pre-check failed:")
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "  • %s\n", issue)
		}
		return fmt.Errorf("fix the issues above and try again")
	}
	fmt.Fprintln(os.Stderr, "✅ All checks passed")

	// Show what will be installed
	appDir := install.AppDir(app.Name)

	fmt.Fprintf(os.Stderr, "\n📦 Installing %s\n", app.Name)
	fmt.Fprintf(os.Stderr, "  Port:    %s (default: %s)\n", port, app.DefaultPort)
	fmt.Fprintf(os.Stderr, "  Path:    %s\n", appDir)
	fmt.Fprintln(os.Stderr)

	// Install
	if err := install.Install(app, opts); err != nil {
		return err
	}

	// Verify
	status, err := install.Status(app.Name)
	if err != nil {
		return fmt.Errorf("installed but failed to verify: %w", err)
	}

	if status == "running" {
		fmt.Fprintln(os.Stderr, "✅ Installation complete!")
		fmt.Fprintf(os.Stderr, "🌐 Access: http://localhost:%s\n", port)
		fmt.Fprintf(os.Stderr, "📁 Config: %s/docker-compose.yml\n", appDir)
		if app.Name == "jellyfin" && mediaFlag == "" {
			fmt.Fprintln(os.Stderr, "\n📂 Media: No media directory mounted.")
			fmt.Fprintln(os.Stderr, "   To add media, reinstall with --media flag:")
			fmt.Fprintf(os.Stderr, "   homebutler install jellyfin --media /path/to/movies\n")
		}
		fmt.Fprintf(os.Stderr, "\n💡 Useful commands:\n")
		fmt.Fprintf(os.Stderr, "  homebutler install status %s\n", app.Name)
		fmt.Fprintf(os.Stderr, "  homebutler logs %s\n", app.Name)
		fmt.Fprintf(os.Stderr, "  homebutler install uninstall %s\n", app.Name)
	} else {
		fmt.Fprintf(os.Stderr, "⚠️  Status: %s (check logs with: homebutler logs %s)\n", status, app.Name)
	}

	return nil
}
