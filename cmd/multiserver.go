package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
)

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
