package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/spf13/cobra"
)

// The calls deploy and upgrade make over the network, swapped out by tests.
var (
	deployToServer     = remote.Deploy
	fetchLatestVersion = remote.FetchLatestVersion
)

func newDeployCmd() *cobra.Command {
	var localBin string

	cmd := &cobra.Command{
		Use:   "deploy",
		Args:  cobra.NoArgs,
		Short: "Install homebutler on remote servers",
		Long: `Deploy homebutler binary to remote servers via SSH.

Use --server to deploy to a specific server, or --all for all remote servers.
Use --local to specify a local binary for air-gapped deployment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			if serverName == "" && !allServers {
				return fmt.Errorf("usage: homebutler deploy --server <name> [--local <binary>]\n       homebutler deploy --all [--local <binary>]")
			}

			var targets []config.ServerConfig
			if allServers {
				for _, s := range cfg.Servers {
					if !s.Local {
						targets = append(targets, s)
					}
				}
			} else {
				server := cfg.FindServer(serverName)
				if server == nil {
					return fmt.Errorf("server %q not found in config", serverName)
				}
				if server.Local {
					return fmt.Errorf("server %q is local, no deploy needed", serverName)
				}
				targets = append(targets, *server)
			}

			if len(targets) == 0 {
				return fmt.Errorf("no remote servers to deploy to")
			}

			releaseVersion := ""
			if localBin == "" {
				latestVersion, err := fetchLatestVersion()
				if err != nil {
					return fmt.Errorf("check latest release: %w", err)
				}
				releaseVersion = latestVersion
				fmt.Fprintf(os.Stderr, "latest release: v%s\n", releaseVersion)
			}

			var results []remote.DeployResult
			for _, srv := range targets {
				fmt.Fprintf(os.Stderr, "deploying to %s (%s)...\n", srv.Name, srv.Host)
				result, err := deployToServer(&srv, localBin, releaseVersion)
				if err != nil {
					results = append(results, remote.DeployResult{
						Server:  srv.Name,
						Status:  "error",
						Message: err.Error(),
					})
					fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", srv.Name, err)
					continue
				}
				results = append(results, *result)
				if result.Status == "error" {
					fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", srv.Name, result.Message)
					continue
				}
				fmt.Fprintf(os.Stderr, "  ✓ %s: %s\n", srv.Name, result.Message)
			}

			if err := output(results, true); err != nil {
				return err
			}
			return deployVerdict(results)
		},
	}

	cmd.Flags().StringVar(&localBin, "local", "", "Use local binary for deploy (air-gapped)")

	return cmd
}

// deployVerdict fails the command when any server did not take the binary.
// Every server's result has already been printed; this is what a script or
// cron job reads.
func deployVerdict(results []remote.DeployResult) error {
	var failed []string
	for _, r := range results {
		if r.Status == "error" {
			failed = append(failed, r.Server)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	noun := "servers"
	if len(results) == 1 {
		noun = "server"
	}
	return fmt.Errorf("deploy failed on %d of %d %s: %s", len(failed), len(results), noun, strings.Join(failed, ", "))
}
