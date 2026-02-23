package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
)

func Execute(version, buildDate string) error {
	if len(os.Args) < 2 {
		printUsage()
		return nil
	}

	// Load config
	cfgPath := config.Resolve(getFlag("--config", ""))
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	jsonOutput := hasFlag("--json")
	serverName := getFlag("--server", "")
	allServers := hasFlag("--all")

	// Multi-server: route to remote execution (skip for deploy — it handles remoting itself)
	isDeployCmd := len(os.Args) >= 2 && os.Args[1] == "deploy"
	if allServers && !isDeployCmd {
		return runAllServers(cfg, os.Args[1:])
	}
	if serverName != "" && !isDeployCmd {
		server := cfg.FindServer(serverName)
		if server == nil {
			return fmt.Errorf("server %q not found in config. Available servers: %s", serverName, listServerNames(cfg))
		}
		if !server.Local {
			remoteArgs := filterFlags(os.Args[1:], "--server", "--all")
			out, err := remote.Run(server, remoteArgs...)
			if err != nil {
				return err
			}
			fmt.Print(string(out))
			return nil
		}
		// Local server — fall through to normal execution
	}

	switch os.Args[1] {
	case "status":
		return runStatus(jsonOutput)
	case "docker":
		return runDocker(jsonOutput)
	case "ports":
		return runPorts(jsonOutput)
	case "network":
		return runNetwork(jsonOutput)
	case "wake":
		return runWake(cfg)
	case "alerts":
		return runAlerts(cfg, jsonOutput)
	case "deploy":
		return runDeploy(cfg)
	case "version":
		fmt.Printf("homebutler %s (built %s)\n", version, buildDate)
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command: %s (run 'homebutler help' for usage)", os.Args[1])
	}
}

func runStatus(jsonOut bool) error {
	info, err := system.Status()
	if err != nil {
		return fmt.Errorf("failed to get system status: %w", err)
	}
	return output(info, jsonOut)
}

func runDocker(jsonOut bool) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: homebutler docker <list|restart|stop|logs> [name]")
	}

	switch os.Args[2] {
	case "list", "ls":
		containers, err := docker.List()
		if err != nil {
			return err
		}
		return output(containers, jsonOut)
	case "restart":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler docker restart <container>")
		}
		return docker.Restart(os.Args[3])
	case "stop":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler docker stop <container>")
		}
		return docker.Stop(os.Args[3])
	case "logs":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler docker logs <container> [lines]")
		}
		lines := "50"
		if len(os.Args) >= 5 {
			lines = os.Args[4]
		}
		return docker.Logs(os.Args[3], lines)
	default:
		return fmt.Errorf("unknown docker command: %s", os.Args[2])
	}
}

func runPorts(jsonOut bool) error {
	openPorts, err := ports.List()
	if err != nil {
		return err
	}
	return output(openPorts, jsonOut)
}

func runNetwork(jsonOut bool) error {
	if len(os.Args) < 3 || os.Args[2] != "scan" {
		return fmt.Errorf("usage: homebutler network scan")
	}
	devices, err := network.ScanWithTimeout(30 * time.Second)
	if err != nil {
		return err
	}
	return output(devices, jsonOut)
}

func runWake(cfg *config.Config) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: homebutler wake <mac-address|name>")
	}
	target := os.Args[2]
	broadcast := "255.255.255.255"

	// Check if target is a name from config
	if wt := cfg.FindWakeTarget(target); wt != nil {
		target = wt.MAC
		if wt.Broadcast != "" {
			broadcast = wt.Broadcast
		}
	}

	// Only use positional arg as broadcast if it's not a flag
	if len(os.Args) >= 4 && !isFlag(os.Args[3]) {
		broadcast = os.Args[3]
	}

	return wake.Send(target, broadcast)
}

func runAlerts(cfg *config.Config, jsonOut bool) error {
	result, err := alerts.Check(&cfg.Alerts)
	if err != nil {
		return fmt.Errorf("failed to check alerts: %w", err)
	}
	return output(result, jsonOut)
}

// serverResult holds the result from a single server.
type serverResult struct {
	Server string          `json:"server"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// runAllServers executes a command on all configured servers in parallel.
func runAllServers(cfg *config.Config, args []string) error {
	if len(cfg.Servers) == 0 {
		return fmt.Errorf("no servers configured. Add servers to your config file")
	}

	remoteArgs := filterFlags(args, "--server", "--all")
	results := make([]serverResult, len(cfg.Servers))
	var wg sync.WaitGroup

	for i, srv := range cfg.Servers {
		wg.Add(1)
		go func(idx int, server config.ServerConfig) {
			defer wg.Done()
			result := serverResult{Server: server.Name}

			if server.Local {
				// Run locally
				out, err := runLocalCommand(remoteArgs)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.Data = json.RawMessage(out)
				}
			} else {
				out, err := remote.Run(&server, remoteArgs...)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.Data = json.RawMessage(out)
				}
			}

			results[idx] = result
		}(i, srv)
	}

	wg.Wait()
	return output(results, true)
}

// runLocalCommand runs homebutler locally and captures JSON output.
func runLocalCommand(args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	switch args[0] {
	case "status":
		info, err := system.Status()
		if err != nil {
			return nil, err
		}
		return json.Marshal(info)
	case "alerts":
		// Use default alert config for local
		alertCfg := &config.AlertConfig{CPU: 90, Memory: 85, Disk: 90}
		result, err := alerts.Check(alertCfg)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "docker":
		if len(args) < 2 || (args[1] != "list" && args[1] != "ls") {
			return nil, fmt.Errorf("only 'docker list' supported with --all")
		}
		containers, err := docker.List()
		if err != nil {
			return nil, err
		}
		return json.Marshal(containers)
	case "ports":
		openPorts, err := ports.List()
		if err != nil {
			return nil, err
		}
		return json.Marshal(openPorts)
	default:
		return nil, fmt.Errorf("command %q not supported with --all", args[0])
	}
}

func filterFlags(args []string, flags ...string) []string {
	skip := make(map[string]bool)
	for _, f := range flags {
		skip[f] = true
	}
	var filtered []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if skip[arg] {
			skipNext = true // skip the flag's value
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

func listServerNames(cfg *config.Config) string {
	if len(cfg.Servers) == 0 {
		return "(none configured)"
	}
	names := make([]string, len(cfg.Servers))
	for i, s := range cfg.Servers {
		names[i] = s.Name
	}
	return fmt.Sprintf("%v", names)
}

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

func output(data any, jsonOut bool) error {
	_ = jsonOut // always JSON for now
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func hasFlag(flag string) bool {
	for _, arg := range os.Args {
		if arg == flag {
			return true
		}
	}
	return false
}

func getFlag(flag, defaultVal string) string {
	for i, arg := range os.Args {
		if arg == flag && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return defaultVal
}

func isFlag(s string) bool {
	return len(s) > 1 && s[0] == '-'
}

func printUsage() {
	fmt.Println(`homebutler — Homelab butler in a single binary 🏠

Usage:
  homebutler <command> [flags]

Commands:
  status              System status (CPU, memory, disk, uptime)
  docker list         List running containers
  docker restart <n>  Restart a container
  docker stop <n>     Stop a container
  docker logs <n>     Show container logs (default: 50 lines)
  wake <mac|name>     Send Wake-on-LAN magic packet
  ports               List open ports with process info
  network scan        Discover devices on local network
  alerts              Check resource thresholds (CPU, memory, disk)
  deploy              Install homebutler on remote servers
  version             Print version
  help                Show this help

Flags:
  --json              Force JSON output
  --server <name>     Run on a specific remote server
  --all               Run on all configured servers in parallel
  --local <path>      Use local binary for deploy (air-gapped)
  --config <path>     Config file path (see Configuration below)

Configuration file is resolved in order:
  1. --config <path>              Explicit flag
  2. $HOMEBUTLER_CONFIG           Environment variable
  3. ~/.config/homebutler/config.yaml   XDG standard
  4. ./homebutler.yaml            Current directory
  If none found, defaults are used.`)
}
