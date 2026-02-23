package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
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
  version             Print version
  help                Show this help

Flags:
  --json              Force JSON output
  --config <path>     Config file path (see Configuration below)

Configuration file is resolved in order:
  1. --config <path>              Explicit flag
  2. $HOMEBUTLER_CONFIG           Environment variable
  3. ~/.config/homebutler/config.yaml   XDG standard
  4. ./homebutler.yaml            Current directory
  If none found, defaults are used.`)
}
