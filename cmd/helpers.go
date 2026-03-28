package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/format"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
)

// Global state set from main.go
var (
	Version   = "dev"
	BuildDate = "unknown"
)

// Global flags
var (
	jsonOutput bool
	serverName string
	allServers bool
	cfgPath    string
)

// cfg holds the loaded config (set in root PersistentPreRun)
var cfg *config.Config

// output renders data as JSON or human-readable format.
func output(data any, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}

	switch v := data.(type) {
	case *system.StatusInfo:
		fmt.Print(format.Status(v))
	case []docker.Container:
		fmt.Print(format.DockerList(v))
	case *docker.ActionResult:
		fmt.Print(format.DockerAction(v.Action, v.Container))
	case []docker.ContainerStats:
		fmt.Print(format.DockerStats(v))
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
	case *backup.BackupResult:
		fmt.Printf("Backup complete: %s\n", v.Archive)
		fmt.Printf("  Services: %s\n", strings.Join(v.Services, ", "))
		fmt.Printf("  Volumes:  %d\n", v.Volumes)
		fmt.Printf("  Size:     %s\n", v.Size)
	case *backup.RestoreResult:
		fmt.Printf("Restore complete from: %s\n", v.Archive)
		fmt.Printf("  Services: %s\n", strings.Join(v.Services, ", "))
		fmt.Printf("  Volumes:  %d\n", v.Volumes)
	case []backup.ListEntry:
		if len(v) == 0 {
			fmt.Println("No backups found.")
		} else {
			fmt.Printf("%-40s %-10s %s\n", "NAME", "SIZE", "CREATED")
			for _, e := range v {
				fmt.Printf("%-40s %-10s %s\n", e.Name, e.Size, e.CreatedAt)
			}
		}
	default:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	return nil
}

// listServerNames returns a formatted list of server names from config.
func listServerNames(c *config.Config) string {
	if len(c.Servers) == 0 {
		return "(none configured)"
	}
	names := make([]string, len(c.Servers))
	for i, s := range c.Servers {
		names[i] = s.Name
	}
	return fmt.Sprintf("%v", names)
}

// filterFlags removes specified flags (and their values) from args.
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
				skipNext = true
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// valueFlags are flags that take a value argument.
var valueFlags = map[string]bool{
	"--server":  true,
	"--config":  true,
	"--local":   true,
	"--port":    true,
	"--service": true,
	"--to":      true,
}

// isFlag checks if a string looks like a CLI flag.
func isFlag(s string) bool {
	return len(s) > 1 && s[0] == '-'
}

// runLocalCommand runs homebutler locally and captures JSON output (for --all).
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

// runAllServers executes a command on all configured servers in parallel.
func runAllServers(c *config.Config, args []string, jsonOut bool) error {
	if len(c.Servers) == 0 {
		return fmt.Errorf("no servers configured. Add servers to your config file")
	}

	remoteArgs := filterFlags(args, "--server", "--all")
	results := make([]serverResult, len(c.Servers))
	var wg sync.WaitGroup

	for i, srv := range c.Servers {
		wg.Add(1)
		go func(idx int, server config.ServerConfig) {
			defer wg.Done()
			result := serverResult{Server: server.Name}

			if server.Local {
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
			var data map[string]interface{}
			if err := json.Unmarshal(r.Data, &data); err != nil {
				fmt.Fprintf(os.Stdout, "📡 %-12s (parse error)\n", r.Server)
				continue
			}
			cpu := getNestedFloat(data, "cpu", "usage_percent")
			mem := getNestedFloat(data, "memory", "usage_percent")
			uptime, _ := data["uptime"].(string)
			disk := getFirstDiskPercent(data)
			fmt.Fprintf(os.Stdout, "📡 %-12s CPU %4.0f%% | Mem %4.0f%% | Disk %4.0f%% | Up %s\n", r.Server, cpu, mem, disk, uptime)
		}
		return nil
	}
	return output(results, true)
}

// serverResult holds the result from a single server.
type serverResult struct {
	Server string          `json:"server"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
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

func getFirstDiskPercent(data map[string]interface{}) float64 {
	if disks, ok := data["disks"].([]interface{}); ok && len(disks) > 0 {
		if d, ok := disks[0].(map[string]interface{}); ok {
			if v, ok := d["usage_percent"].(float64); ok {
				return v
			}
		}
	}
	return 0
}
