package cmd

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

func runDeploy(cfg *config.Config) error {
	serverName := getFlag("--server", "")
	localBin := getFlag("--local", "")
	allServers := hasFlag("--all")

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

	var results []remote.DeployResult
	for _, srv := range targets {
		fmt.Fprintf(os.Stderr, "deploying to %s (%s)...\n", srv.Name, srv.Host)
		result, err := remote.Deploy(&srv, localBin)
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
		fmt.Fprintf(os.Stderr, "  ✓ %s: %s\n", srv.Name, result.Message)
	}

	return output(results, true)
}
