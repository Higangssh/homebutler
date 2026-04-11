package cmd

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "homebutler",
	Short: "Homelab butler in a single binary 🏠",
	Long: `homebutler — Homelab butler in a single binary 🏠

Monitor, manage, and automate your homelab servers with a single CLI.
Supports Docker, Wake-on-LAN, network scanning, backups, alerts, and more.

Configuration file is resolved in order:
  1. --config <path>              Explicit flag
  2. $HOMEBUTLER_CONFIG           Environment variable
  3. ~/.config/homebutler/config.yaml   XDG standard
  4. ./homebutler.yaml            Current directory
  If none found, defaults are used.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Force JSON output")
	rootCmd.PersistentFlags().StringVar(&serverName, "server", "", "Run on a specific remote server")
	rootCmd.PersistentFlags().BoolVar(&allServers, "all", false, "Run on all configured servers in parallel")
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "Config file path")

	rootCmd.AddCommand(
		newStatusCmd(),
		newDockerCmd(),
		newPortsCmd(),
		newProcessesCmd(),
		newNetworkCmd(),
		newWakeCmd(),
		newNotifyCmd(),
		newAlertsCmd(),
		newTrustCmd(),
		newBackupCmd(),
		newRestoreCmd(),
		newInstallCmd(),
		newDeployCmd(),
		newUpgradeCmd(),
		newServeCmd(),
		newMCPCmd(),
		newVersionCmd(),
		newWatchCmd(),
		newInitCmd(),
	)
}

// loadConfig loads the config file.
func loadConfig() error {
	resolved := config.Resolve(cfgPath)
	var err error
	cfg, err = config.Load(resolved)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}
	return nil
}

// maybeRouteRemote checks --server and --all flags and routes to remote servers if needed.
// Returns true if the command was handled remotely.
// skipRemote commands (deploy, upgrade) handle their own remote routing.
func maybeRouteRemote() (bool, error) {
	if allServers {
		remoteArgs := filterFlags(os.Args[1:], "--server", "--all", "--config")
		return true, runAllServers(cfg, remoteArgs, jsonOutput)
	}
	if serverName != "" {
		server := cfg.FindServer(serverName)
		if server == nil {
			return true, fmt.Errorf("server %q not found in config. Available servers: %s", serverName, listServerNames(cfg))
		}
		if !server.Local {
			remoteArgs := filterFlags(os.Args[1:], "--server", "--all", "--config")
			out, err := remote.Run(server, remoteArgs...)
			if err != nil {
				return true, err
			}
			fmt.Print(string(out))
			return true, nil
		}
	}
	return false, nil
}

// Execute is the entry point called from main.go.
func Execute(version, buildDate string) error {
	Version = version
	BuildDate = buildDate
	return rootCmd.Execute()
}
