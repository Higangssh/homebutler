package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/install"
)

func runInstall() error {
	if len(os.Args) < 3 {
		return printInstallUsage()
	}

	subCmd := os.Args[2]

	switch subCmd {
	case "list", "ls":
		return runInstallList()
	case "status":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler install status <app>")
		}
		return runInstallStatus(os.Args[3])
	case "uninstall", "rm":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler install uninstall <app>")
		}
		return runInstallUninstall(os.Args[3])
	case "purge":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler install purge <app>")
		}
		return runInstallPurge(os.Args[3])
	default:
		return runInstallApp(subCmd)
	}
}

func runInstallList() error {
	apps := install.List()
	fmt.Fprintf(os.Stderr, "📦 Available apps (%d):\n\n", len(apps))
	for _, app := range apps {
		fmt.Fprintf(os.Stderr, "  %-20s %s\n", app.Name, app.Description)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Usage: homebutler install <app>")
	return nil
}

func runInstallApp(appName string) error {
	app, ok := install.Registry[appName]
	if !ok {
		available := make([]string, 0)
		for name := range install.Registry {
			available = append(available, name)
		}
		return fmt.Errorf("unknown app %q. Available: %s", appName, strings.Join(available, ", "))
	}

	// Parse options
	opts := install.InstallOptions{
		Port: getFlag("--port", ""),
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
		fmt.Fprintf(os.Stderr, "\n💡 Useful commands:\n")
		fmt.Fprintf(os.Stderr, "  homebutler install status %s\n", app.Name)
		fmt.Fprintf(os.Stderr, "  homebutler logs %s\n", app.Name)
		fmt.Fprintf(os.Stderr, "  homebutler install uninstall %s\n", app.Name)
	} else {
		fmt.Fprintf(os.Stderr, "⚠️  Status: %s (check logs with: homebutler logs %s)\n", status, app.Name)
	}

	return nil
}

func runInstallStatus(appName string) error {
	status, err := install.Status(appName)
	if err != nil {
		return err
	}
	icon := "🔴"
	if status == "running" {
		icon = "🟢"
	}
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", icon, appName, status)
	return nil
}

func runInstallUninstall(appName string) error {
	fmt.Fprintf(os.Stderr, "🛑 Stopping %s...\n", appName)
	if err := install.Uninstall(appName); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "✅ Stopped and removed containers")
	fmt.Fprintf(os.Stderr, "💡 Data preserved at: %s\n", install.GetInstalledPath(appName))
	fmt.Fprintf(os.Stderr, "   To delete everything: homebutler install purge %s\n", appName)
	return nil
}

func runInstallPurge(appName string) error {
	fmt.Fprintf(os.Stderr, "⚠️  Purging %s (containers + data)...\n", appName)
	if err := install.Purge(appName); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "✅ Completely removed")
	return nil
}

func printInstallUsage() error {
	fmt.Fprintln(os.Stderr, "Usage: homebutler install <app> [options]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  homebutler install <app>                Install an app")
	fmt.Fprintln(os.Stderr, "  homebutler install <app> --port 8080    Custom port")
	fmt.Fprintln(os.Stderr, "  homebutler install list                 List available apps")
	fmt.Fprintln(os.Stderr, "  homebutler install status <app>         Check app status")
	fmt.Fprintln(os.Stderr, "  homebutler install uninstall <app>      Stop (keep data)")
	fmt.Fprintln(os.Stderr, "  homebutler install purge <app>          Stop + delete data")
	return nil
}
