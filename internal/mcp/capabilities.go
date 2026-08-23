package mcp

type capabilityRisk string

const (
	riskRead        capabilityRisk = "read"
	riskWrite       capabilityRisk = "write"
	riskDestructive capabilityRisk = "destructive"
)

// targetKind is what a tool can be pointed at. A bool could only ask "is this a
// named SSH server from servers:", which is the only kind of target homebutler
// has had so far. An API-backed target has no agent on the far side and a
// different way of being addressed, so the question has to be open-ended before
// 1.0 freezes the answer.
type targetKind string

const (
	targetLocal  targetKind = "local"  // this machine; no target argument
	targetServer targetKind = "server" // a named SSH server from servers:
)

type capability struct {
	tool    toolDef
	risk    capabilityRisk
	targets []targetKind
}

// supports reports whether the tool can be pointed at kind.
func (c capability) supports(kind targetKind) bool {
	for _, t := range c.targets {
		if t == kind {
			return true
		}
	}
	return false
}

// capabilityFor returns the registry entry for a tool name.
func capabilityFor(name string) (capability, bool) {
	for _, c := range capabilityRegistry {
		if c.tool.Name == name {
			return c, true
		}
	}
	return capability{}, false
}

func toolDefinitions() []toolDef {
	defs := make([]toolDef, 0, len(capabilityRegistry))
	for _, c := range capabilityRegistry {
		defs = append(defs, c.tool)
	}
	return defs
}

var capabilityRegistry = []capability{
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "system_status",
			Description: "Get system status including CPU, memory, disk usage, and uptime",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "docker_list",
			Description: "List Docker containers with their status, image, and ports",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "docker_restart",
			Description: "Restart a Docker container by name",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"name":   {Type: "string", Description: "Container name to restart"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		risk:    riskDestructive,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "docker_stop",
			Description: "Stop a Docker container by name",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"name":   {Type: "string", Description: "Container name to stop"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "docker_logs",
			Description: "Get logs from a Docker container",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"name":   {Type: "string", Description: "Container name to get logs from"},
					"lines":  {Type: "string", Description: "Number of log lines to return (default: 50)"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "docker_stats",
			Description: "Get resource usage statistics (CPU, memory, network, block I/O) for all running Docker containers",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "wake",
			Description: "Send a Wake-on-LAN magic packet to wake a machine",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"target":    {Type: "string", Description: "MAC address or configured device name"},
					"broadcast": {Type: "string", Description: "Broadcast address (default: 255.255.255.255)"},
				},
				Required: []string{"target"},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "open_ports",
			Description: "List open network ports with associated process information",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "network_scan",
			Description: "Scan the local network to discover devices (IP, MAC, hostname)",
			InputSchema: inputSchema{
				Type: "object",
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "alerts",
			Description: "Check resource alerts for CPU, memory, and disk usage against configured thresholds",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "inventory_scan",
			Description: "Collect server inventory/topology including system status, Docker containers, app ports, and system ports",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "inventory_export",
			Description: "Export server inventory/topology as a Mermaid diagram locally, or JSON locally/remotely",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"format": {Type: "string", Description: "Export format: mermaid (default, local) or json"},
					"server": {Type: "string", Description: "Remote server name from config (optional; remote supports format=json)"},
				},
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "report",
			Description: "Generate a butler-style health report with snapshot comparison, warnings, notable changes, and suggested actions",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"keep":    {Type: "number", Description: "Number of snapshots to retain (default: 30)"},
					"no_save": {Type: "boolean", Description: "Preview without writing a snapshot"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "doctor",
			Description: "Run a read-only diagnosis for resource pressure, stopped containers, public ports, backup hygiene, notifications, and report baseline readiness",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"backup_max_age_hours": {Type: "number", Description: "Warn when the latest backup is older than this many hours (default: 168)"},
					"server":               {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		// Write rather than read: CheckTargets records the new container state
		// and saves any incident it detects, so a caller cannot treat this as a
		// free query the way system_status is.
		risk:    riskWrite,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "watch_check",
			Description: "Run a one-shot restart check on watched targets and report restarts detected since the last check. Only docker targets can be inspected this way; systemd and pm2 targets are reported as skipped rather than assumed healthy",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "backup_create",
			Description: "Create a Docker compose backup archive for all services or one service",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"service": {Type: "string", Description: "Specific service to back up (optional)"},
					"to":      {Type: "string", Description: "Custom backup destination directory (optional)"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "backup_list",
			Description: "List existing backup archives in the configured backup directory",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "backup_drill",
			Description: "Verify a backup by booting an app in an isolated Docker environment and checking that it responds",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"app":     {Type: "string", Description: "App/service to drill (required unless all=true)"},
					"all":     {Type: "boolean", Description: "Drill all supported apps in the backup"},
					"archive": {Type: "string", Description: "Specific backup archive to verify (optional)"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		risk:    riskDestructive,
		targets: []targetKind{targetLocal, targetServer},
		tool: toolDef{
			Name:        "backup_restore",
			Description: "Restore Docker volumes from a backup archive. Destructive: confirm intent before calling.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"archive": {Type: "string", Description: "Backup archive path to restore"},
					"service": {Type: "string", Description: "Specific service to restore (optional)"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"archive"},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "install_list",
			Description: "List available self-hosted apps that can be installed",
			InputSchema: inputSchema{
				Type: "object",
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "install_app",
			Description: "Install a self-hosted app via docker compose. Pre-checks docker, ports, and duplicates automatically.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"app":  {Type: "string", Description: "App name (e.g. uptime-kuma, vaultwarden)"},
					"port": {Type: "string", Description: "Custom host port (optional, uses default if omitted)"},
				},
				Required: []string{"app"},
			},
		},
	},
	{
		risk:    riskRead,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "install_status",
			Description: "Check the status of an installed app",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"app": {Type: "string", Description: "App name"},
				},
				Required: []string{"app"},
			},
		},
	},
	{
		risk:    riskWrite,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "install_uninstall",
			Description: "Stop an installed app and remove its containers. Data is preserved.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"app": {Type: "string", Description: "App name"},
				},
				Required: []string{"app"},
			},
		},
	},
	{
		risk:    riskDestructive,
		targets: []targetKind{targetLocal},
		tool: toolDef{
			Name:        "install_purge",
			Description: "Stop an installed app and delete all data including containers, config, and volumes.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"app": {Type: "string", Description: "App name"},
				},
				Required: []string{"app"},
			},
		},
	},
}
