package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

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
	rootCmd.PersistentFlags().BoolVarP(&verboseOutput, "verbose", "v", false, "Show detailed error information")
	rootCmd.PersistentFlags().StringVar(&serverName, "server", "", "Run on a specific remote server (commands that support it)")
	rootCmd.PersistentFlags().BoolVar(&allServers, "all", false, "Run on all configured servers in parallel (commands that support it)")
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
		newInventoryCmd(),
		newReportCmd(),
		newDoctorCmd(),
		newConfigCmd(),
		newProxmoxCmd(),
	)

	// No command below sets a PersistentPreRun of its own, which would
	// replace this one for it and its subcommands.
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return refuseIgnoredRemoteFlags(cmd)
	}
}

// remoteFlags is every command that does something with --server or --all,
// keyed by its path below the root, and which of the two it takes.
//
// They are global flags, so cobra hands them to every command, and a command
// that never looks at them runs here instead: `install jellyfin --server rpi5`
// installed it on this machine. Anything not listed is refused before it
// runs, so a command added later is refused until someone decides it can
// reach another server, rather than quietly answering about this one.
//
// The ones that route through maybeRouteRemote take both here; which of them
// --all can run on every server is decided where it fans out.
var remoteFlags = map[string][]string{
	"status":         {"server", "all"},
	"alerts":         {"server", "all"},
	"ports":          {"server", "all"},
	"processes":      {"server", "all"},
	"report":         {"server", "all"},
	"doctor":         {"server", "all"},
	"wake":           {"server", "all"},
	"network scan":   {"server", "all"},
	"docker list":    {"server", "all"},
	"docker restart": {"server", "all"},
	"docker stop":    {"server", "all"},
	"docker logs":    {"server", "all"},
	"docker stats":   {"server", "all"},
	"docker top":     {"server", "all"},
	"docker inspect": {"server", "all"},
	"backup":         {"server", "all"},
	"backup list":    {"server", "all"},
	"backup drill":   {"server", "all"},
	"restore":        {"server", "all"},
	"deploy":         {"server", "all"},
	// upgrade always covers every remote server, so --all asks for what it
	// does; --server would upgrade all of them while naming one.
	"upgrade": {"all"},
}

// localOnlyReasons says why a command does not take --server or --all where
// "runs on this machine only" would be wrong. A subcommand inherits its
// parent's.
var localOnlyReasons = map[string]string{
	"upgrade": "upgrades this machine and every remote server, or only this one with --local",
	"trust":   "takes the server to trust as its argument",
	"proxmox": "reaches Proxmox through its API, chosen with --endpoint",
	// The scripts are a catalogue built into the binary, not an API call, so
	// they take the default back from the proxmox reason above.
	"proxmox script": "runs on this machine only",
	"watch tui":      "already shows every configured server",
}

// refuseIgnoredRemoteFlags fails a command given --server or --all that it
// would otherwise ignore, before it does anything. It reads the flags' values
// rather than whether they were set, so `backup drill --all`, whose own --all
// shadows the global one, is not mistaken for it.
func refuseIgnoredRemoteFlags(cmd *cobra.Command) error {
	var given []string
	if serverName != "" {
		given = append(given, "server")
	}
	if allServers {
		given = append(given, "all")
	}
	if len(given) == 0 {
		return nil
	}

	path := commandPath(cmd)
	// help and completion describe the CLI, not a machine.
	if root, _, _ := strings.Cut(path, " "); root == "help" || root == "completion" || root == cobra.ShellCompRequestCmd || root == cobra.ShellCompNoDescRequestCmd {
		return nil
	}

	var refused []string
	for _, flag := range given {
		if !slices.Contains(remoteFlags[path], flag) {
			refused = append(refused, "--"+flag)
		}
	}
	if len(refused) == 0 {
		return nil
	}

	verb := "is"
	if len(refused) > 1 {
		verb = "are"
	}
	return fmt.Errorf("%s %s; %s %s not supported", path, localOnlyReason(path), strings.Join(refused, " and "), verb)
}

// commandPath is a command's path below the root, such as "inventory scan".
func commandPath(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
}

func localOnlyReason(path string) string {
	for p := path; ; {
		if reason, ok := localOnlyReasons[p]; ok {
			return reason
		}
		i := strings.LastIndex(p, " ")
		if i < 0 {
			return "runs on this machine only"
		}
		p = p[:i]
	}
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
	// So a remote command that fails for being older than this one can say
	// which two versions are involved.
	remote.LocalVersion = version
	return formatCommandError(rootCmd.Execute())
}
