package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/server"
	"github.com/Higangssh/homebutler/internal/service"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var host string
	var port int
	var demo bool
	var token string

	cmd := &cobra.Command{
		Use:   "serve",
		Args:  cobra.NoArgs,
		Short: "Web dashboard (default port 8080)",
		Long: `Start the homebutler web dashboard. Use --demo for realistic demo data without real system calls.

The API token comes from the first of these that is set:
  1. --token                visible in ps to every user on the machine
  2. web.token              in the config file
  3. $HOMEBUTLER_TOKEN      for containers, where the platform passes secrets
                            as environment variables. An empty value is unset.

A token from the environment is never written to the config file.

The dashboard runs for as long as this command does. To have the host keep it
running: homebutler serve install`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			var note string
			token, note = resolveWebToken(token, cfg, os.LookupEnv)
			if note != "" {
				fmt.Fprintln(os.Stderr, note)
			}
			// serve runs docker, systemctl and ssh. None of them needs the
			// dashboard's token, and each would otherwise inherit it.
			_ = os.Unsetenv(webTokenEnv)

			// The image exists to be reached from another machine, which is
			// exactly when an unauthenticated dashboard is wrong. Outside a
			// container this stays a warning on the settings screen: somebody
			// running it on a LAN behind their own reverse proxy has made a
			// choice, and a container published to a network has not.
			if token == "" && system.InContainer() && !isLoopback(host) {
				return fmt.Errorf("refusing to serve %s from a container without a token: this dashboard would be reachable from the network with nothing in front of it\n  → set HOMEBUTLER_TOKEN in the container's environment, or bind 127.0.0.1 and put a proxy in front", host)
			}

			srv := server.New(cfg, host, port, demo)
			srv.SetVersion(Version)
			if token != "" {
				srv.SetToken(token)
			}
			return srv.Run()
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host to bind to")
	cmd.Flags().IntVar(&port, "port", 8080, "Port for the web dashboard")
	cmd.Flags().BoolVar(&demo, "demo", false, "Run with realistic demo data (no real system calls)")
	cmd.Flags().StringVar(&token, "token", "", "Bearer token for API authentication (prefer web.token in the config file, or $HOMEBUTLER_TOKEN in a container)")

	cmd.AddCommand(newServeInstallCmd(), newServeUninstallCmd(), newServeInstalledCmd())
	return cmd
}

// webTokenEnv is how a container platform hands serve its token. Unraid
// templates, compose environment: blocks and the Portainer and TrueNAS forms
// all pass secrets this way and mask them in their UI; asking them for a
// chmod-600 file on the host instead fails the first start.
const webTokenEnv = "HOMEBUTLER_TOKEN"

// resolveWebToken picks the dashboard token: the flag, then web.token, then
// $HOMEBUTLER_TOKEN. The note, when there is one, says why the variable was
// not used, and goes to stderr.
//
// --token is the older spelling and stays, but it puts the token in ps output
// for every user on the machine, so the config file is where it belongs and
// where an installed unit can read it without carrying a copy.
//
// The variable comes last because it is the weakest place to keep a secret —
// docker inspect shows it, and so does every process that inherits it — and
// because a variable left exported in a shell profile should not quietly
// replace a token somebody wrote into the config on purpose. It is read here
// and never copied into cfg, which is what keeps the settings screen from
// writing it to config.yaml.
func resolveWebToken(flag string, cfg *config.Config, lookupEnv func(string) (string, bool)) (token, note string) {
	env, envSet := lookupEnv(webTokenEnv)
	env = strings.TrimSpace(env)

	from := ""
	switch {
	case flag != "":
		token, from = flag, "--token"
	case cfg != nil && cfg.Web.Token != "":
		token, from = cfg.Web.Token, "web.token in "+cfg.Path
	}
	if token != "" {
		if env != "" && env != token {
			note = fmt.Sprintf("%s is set but ignored: %s takes precedence", webTokenEnv, from)
		}
		return token, note
	}

	if envSet && env == "" {
		// An empty token would mean no authentication. Nobody sets a
		// variable to ask for that; an unfilled template field does.
		return "", fmt.Sprintf("%s is set but empty, so it is ignored and the dashboard has no token", webTokenEnv)
	}
	return env, ""
}

// installBindRefusal blocks installing an unauthenticated dashboard on an
// address other machines can reach.
//
// A foreground serve is exposed for as long as somebody is watching the
// terminal it runs in, which is why its own rule only fires inside a
// container. An installed one is exposed until somebody uninstalls it, and
// months later nobody remembers it is up — so the same leniency is wrong here.
//
// The message names both ways out, because which one is right depends on
// something homebutler cannot see: whether the operator meant to publish it.
func installBindRefusal(host, token, configPath string) error {
	if token != "" || isLoopback(host) {
		return nil
	}
	return fmt.Errorf("refusing to install a dashboard on %s with no token: it would answer anyone who can reach this machine, and it stays up until you uninstall it\n  → set web.token in %s, or install on 127.0.0.1", host, configPath)
}

// isLoopback reports whether binding host exposes the dashboard only to the
// machine it runs on.
func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

func newServeInstallCmd() *cobra.Command {
	var host string
	var port int
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the dashboard with the host's service supervisor",
		Long: `Write a service unit so the dashboard survives logout and reboot.

Like watch install, this is a user-level unit — a systemd user unit on Linux, a
launchd agent on macOS — because the config it reads lives in the invoking
user's home directory.

The unit records the address and nothing else. The API token is read from the
config file at startup: a unit file is world-readable and --token is visible in
ps to every user on the machine, so neither is a place to keep one.

Undo with: homebutler serve uninstall`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			if err := installBindRefusal(host, cfg.Web.Token, cfg.Path); err != nil {
				return err
			}

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

			unit := service.Serve(host, port)
			// A unit installed with --config has to keep reading that file.
			// Without this the service resolves the default path instead,
			// finds no token there, and serves an unauthenticated dashboard
			// from a command that refused to do exactly that.
			if cfgPath != "" {
				unit.Args = append(unit.Args, "--config", cfg.Path)
			}

			path := service.UnitPath(kind, home, unit)
			if service.Installed(path) && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
			}
			// launchctl bootstrap fails on a service that is already loaded, so
			// overwriting the file is not enough to make launchd read it — the
			// old address stays live. Unload first; the error is ignored
			// because "was not loaded" is the normal case here.
			if service.Installed(path) {
				_ = service.Run(service.StopCommand(kind, path, unit))
			}
			if err := service.Write(path, service.Render(kind, exe, home, unit)); err != nil {
				return err
			}
			if err := service.Run(service.StartCommand(kind, path, unit)); err != nil {
				// The unit is written; only activation failed. Say both, so the
				// operator knows what to remove and what to run by hand.
				return fmt.Errorf("wrote %s but could not start it: %w", path, err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "✅ %s unit written to %s\n", kind, path)
			fmt.Fprintf(out, "✅ enabled and started on http://%s:%d\n", host, port)
			if cfg.Web.Token == "" {
				fmt.Fprintf(out, "\n⚠️  No web.token in %s, so the dashboard is read-only and reachable only from this machine.\n", cfg.Path)
			}
			if note := service.LingerNote(kind); note != "" {
				fmt.Fprintf(out, "\n⚠️  %s\n", note)
			}
			fmt.Fprintf(out, "\nUndo with: homebutler serve uninstall\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host the installed dashboard binds to")
	cmd.Flags().IntVar(&port, "port", 8080, "Port the installed dashboard binds to")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing unit")
	return cmd
}

func newServeUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the service unit installed by serve install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, path, err := serveUnitPath()
			if err != nil {
				return err
			}
			if !service.Installed(path) {
				return fmt.Errorf("no unit installed at %s", path)
			}

			// Stopping can fail on a unit that is already stopped, which is not
			// a reason to leave the file behind.
			stopErr := service.Run(service.StopCommand(kind, path, service.Serve("", 0)))
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

func newServeInstalledCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "installed",
		Short: "Report whether the dashboard is registered with the supervisor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, path, err := serveUnitPath()
			if err != nil {
				return err
			}
			installed := service.Installed(path)

			// The address comes out of the unit the supervisor actually reads,
			// so this cannot report a port that nothing is listening on.
			address := ""
			if installed {
				if data, err := os.ReadFile(path); err == nil {
					if host, port, ok := service.Address(service.UnitArgs(kind, string(data))); ok {
						address = fmt.Sprintf("http://%s:%d", host, port)
					}
				}
			}

			if jsonOutput {
				return output(map[string]any{
					"installed": installed,
					"kind":      string(kind),
					"path":      path,
					"address":   address,
				}, true)
			}

			out := cmd.OutOrStdout()
			if !installed {
				fmt.Fprintf(out, "Not installed. %s would be written to %s\n", kind, path)
				fmt.Fprintf(out, "    homebutler serve install\n")
				return nil
			}
			fmt.Fprintf(out, "Installed: %s (%s)\n", path, kind)
			if address != "" {
				fmt.Fprintf(out, "Serving:   %s\n", address)
			} else {
				fmt.Fprintf(out, "Serving:   unknown — %s does not state an address\n", path)
			}
			return nil
		},
	}
}

// serveUnitPath resolves where the dashboard's unit belongs on this host. The
// label does not depend on the address, so an empty Serve is enough to name it.
func serveUnitPath() (service.Kind, string, error) {
	kind, err := service.Detect()
	if err != nil {
		return "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return kind, service.UnitPath(kind, home, service.Serve("", 0)), nil
}
