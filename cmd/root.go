package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/format"
	"github.com/Higangssh/homebutler/internal/mcp"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/tui"
	"github.com/Higangssh/homebutler/internal/wake"
)

func Execute(version, buildDate string) error {
	if len(os.Args) < 2 {
		printUsage()
		return nil
	}

	// init command — runs before config loading (it creates the config)
	if os.Args[1] == "init" {
		return runInit()
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

	// watch command — always monitors all configured servers
	if os.Args[1] == "watch" {
		return tui.Run(cfg, nil)
	}

	// Multi-server: route to remote execution (skip for deploy — it handles remoting itself)
	isDeployCmd := len(os.Args) >= 2 && os.Args[1] == "deploy"
	if allServers && !isDeployCmd {
		return runAllServers(cfg, os.Args[1:], jsonOutput)
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
	case "processes":
		return runProcesses(jsonOutput)
	case "network":
		return runNetwork(jsonOutput)
	case "wake":
		return runWake(cfg, jsonOutput)
	case "alerts":
		return runAlerts(cfg, jsonOutput)
	case "trust":
		return runTrust(cfg)
	case "deploy":
		return runDeploy(cfg)
	case "serve":
		return runServe(cfg)
	case "mcp":
		demo := hasFlag("--demo")
		return mcp.NewServer(cfg, version, demo).Run()
	case "version", "-v", "--version":
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

func runDocker(jsonOutput bool) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: homebutler docker <list|restart|stop|logs> [name]")
	}

	switch os.Args[2] {
	case "list", "ls":
		containers, err := docker.List()
		if err != nil {
			return err
		}
		return output(containers, jsonOutput)
	case "restart":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler docker restart <container>")
		}
		result, err := docker.Restart(os.Args[3])
		if err != nil {
			return err
		}
		return output(result, jsonOutput)
	case "stop":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler docker stop <container>")
		}
		result, err := docker.Stop(os.Args[3])
		if err != nil {
			return err
		}
		return output(result, jsonOutput)
	case "logs":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: homebutler docker logs <container> [lines]")
		}
		lines := "50"
		if len(os.Args) >= 5 {
			lines = os.Args[4]
		}
		result, err := docker.Logs(os.Args[3], lines)
		if err != nil {
			return err
		}
		return output(result, jsonOutput)
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

func runProcesses(jsonOut bool) error {
	procs, err := system.TopProcesses(10)
	if err != nil {
		return err
	}
	return output(procs, jsonOut)
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

func runWake(cfg *config.Config, jsonOut bool) error {
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

	result, err := wake.Send(target, broadcast)
	if err != nil {
		return err
	}
	return output(result, jsonOut)
}

func runAlerts(cfg *config.Config, jsonOut bool) error {
	result, err := alerts.Check(&cfg.Alerts)
	if err != nil {
		return fmt.Errorf("failed to check alerts: %w", err)
	}
	return output(result, jsonOut)
}

func runTrust(cfg *config.Config) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: homebutler trust <server> [--reset]")
	}
	serverName := os.Args[2]
	reset := hasFlag("--reset")

	server := cfg.FindServer(serverName)
	if server == nil {
		return fmt.Errorf("server %q not found in config. Available servers: %s", serverName, listServerNames(cfg))
	}

	if reset {
		fmt.Fprintf(os.Stderr, "removing old host keys for %s...\n", server.Name)
		if err := remote.RemoveHostKeys(server); err != nil {
			return fmt.Errorf("failed to remove old keys: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "connecting to %s (%s:%d)...\n", server.Name, server.Host, server.SSHPort())
	err := remote.TrustServer(server, func(fingerprint string) bool {
		fmt.Fprintf(os.Stderr, "host key fingerprint: %s\n", fingerprint)
		fmt.Fprint(os.Stderr, "trust this host? (y/n): ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		return strings.TrimSpace(strings.ToLower(answer)) == "y"
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "host key for %s added to known_hosts\n", server.Name)
	return nil
}

// serverResult holds the result from a single server.
type serverResult struct {
	Server string          `json:"server"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// runAllServers executes a command on all configured servers in parallel.
func runAllServers(cfg *config.Config, args []string, jsonOut bool) error {
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

	if !jsonOut {
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(os.Stdout, "❌ %-12s %s\n", r.Server, r.Error)
				continue
			}
			// Parse JSON data to extract status fields
			var data map[string]interface{}
			if err := json.Unmarshal(r.Data, &data); err != nil {
				fmt.Fprintf(os.Stdout, "📡 %-12s (parse error)\n", r.Server)
				continue
			}
			cpu := getNestedFloat(data, "cpu", "usage_percent")
			mem := getNestedFloat(data, "memory", "usage_percent")
			uptime, _ := data["uptime"].(string)
			disk := 0.0
			if disks, ok := data["disks"].([]interface{}); ok && len(disks) > 0 {
				if d, ok := disks[0].(map[string]interface{}); ok {
					disk, _ = d["usage_percent"].(float64)
				}
			}
			fmt.Fprintf(os.Stdout, "📡 %-12s CPU %4.0f%% | Mem %4.0f%% | Disk %4.0f%% | Up %s\n", r.Server, cpu, mem, disk, uptime)
		}
		return nil
	}
	return output(results, true)
}

func getNestedFloat(data map[string]interface{}, keys ...string) float64 {
	current := data
	for i, key := range keys {
		if i == len(keys)-1 {
			if v, ok := current[key].(float64); ok {
				return v
			}
			return 0
		}
		if next, ok := current[key].(map[string]interface{}); ok {
			current = next
		} else {
			return 0
		}
	}
	return 0
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

// valueFlags are flags that take a value argument.
var valueFlags = map[string]bool{
	"--server": true,
	"--config": true,
	"--local":  true,
	"--port":   true,
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
			if valueFlags[arg] {
				skipNext = true // skip the flag's value too
			}
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
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}

	// Human-readable output
	switch v := data.(type) {
	case *system.StatusInfo:
		fmt.Print(format.Status(v))
	case []docker.Container:
		fmt.Print(format.DockerList(v))
	case *docker.ActionResult:
		fmt.Print(format.DockerAction(v.Action, v.Container))
	case *docker.LogsResult:
		fmt.Printf("=== %s (last %s lines) ===\n%s\n", v.Container, v.Lines, v.Logs)
	case *alerts.AlertResult:
		fmt.Print(format.Alerts(v))
	case []ports.PortInfo:
		fmt.Print(format.Ports(v))
	case []network.Device:
		fmt.Print(format.NetworkScan(v))
	case *wake.WakeResult:
		fmt.Print(format.WakeResult(v.MAC, v.Broadcast))
	default:
		// Fallback to JSON for unknown types (deploy results, multi-server, etc.)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	return nil
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
  init                Interactive setup wizard (creates config)
  status              System status (CPU, memory, disk, uptime)
  watch               TUI dashboard (monitors all configured servers)
  docker list         List running containers
  docker restart <n>  Restart a container
  docker stop <n>     Stop a container
  docker logs <n>     Show container logs (default: 50 lines)
  wake <mac|name>     Send Wake-on-LAN magic packet
  ports               List open ports with process info
  network scan        Discover devices on local network
  alerts              Check resource thresholds (CPU, memory, disk)
  trust <server>      Trust a remote server's SSH host key
  deploy              Install homebutler on remote servers
  serve               Web dashboard (default port 8080)
  mcp                 Start MCP server (JSON-RPC over stdio)
  version             Print version
  help                Show this help

Flags:
  --json              Force JSON output
  --server <name>     Run on a specific remote server
  --all               Run on all configured servers in parallel
  --reset             Remove old host key before re-trusting (use with trust)
  --local <path>      Use local binary for deploy (air-gapped)
  --port <number>     Port for serve command (default: 8080)
  --demo              Run serve with realistic demo data (no real system calls)
  --config <path>     Config file path (see Configuration below)

Configuration file is resolved in order:
  1. --config <path>              Explicit flag
  2. $HOMEBUTLER_CONFIG           Environment variable
  3. ~/.config/homebutler/config.yaml   XDG standard
  4. ./homebutler.yaml            Current directory
  If none found, defaults are used.`)
}
