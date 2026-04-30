package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/install"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
)

// JSON-RPC 2.0 types

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP protocol types

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    capInfo    `json:"capabilities"`
	ServerInfo      serverInfo `json:"serverInfo"`
}

type capInfo struct {
	Tools *toolsCap `json:"tools,omitempty"`
}

type toolsCap struct{}

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string             `json:"type"`
	Properties map[string]propDef `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
}

type propDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type toolsListResult struct {
	Tools []toolDef `json:"tools"`
}

type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolsCallResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Server is the MCP server.
type Server struct {
	cfg     *config.Config
	version string
	demo    bool
	in      io.Reader
	out     io.Writer
}

// NewServer creates a new MCP server.
func NewServer(cfg *config.Config, version string, demo ...bool) *Server {
	d := len(demo) > 0 && demo[0]
	return &Server{
		cfg:     cfg,
		version: version,
		demo:    d,
		in:      os.Stdin,
		out:     os.Stdout,
	}
}

// Run starts the MCP server, reading JSON-RPC messages from stdin and writing responses to stdout.
func (s *Server) Run() error {
	scanner := bufio.NewScanner(s.in)
	// Increase buffer for large messages
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "parse error")
			continue
		}

		s.handleRequest(&req)
	}

	return scanner.Err()
}

func (s *Server) handleRequest(req *jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, initializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities:    capInfo{Tools: &toolsCap{}},
			ServerInfo:      serverInfo{Name: "homebutler", Version: s.version},
		})
	case "notifications/initialized":
		// Notification — no response needed
	case "tools/list":
		s.writeResult(req.ID, toolsListResult{Tools: toolDefinitions()})
	case "tools/call":
		s.handleToolCall(req)
	default:
		if req.ID != nil {
			s.writeError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		}
	}
}

func (s *Server) handleToolCall(req *jsonRPCRequest) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "invalid params")
		return
	}

	result, toolErr := s.executeTool(params.Name, params.Arguments)
	if toolErr != nil {
		s.writeResult(req.ID, toolsCallResult{
			Content: []contentItem{{Type: "text", Text: toolErr.Error()}},
			IsError: true,
		})
		return
	}

	data, err := json.Marshal(result)
	if err != nil {
		s.writeResult(req.ID, toolsCallResult{
			Content: []contentItem{{Type: "text", Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		})
		return
	}

	s.writeResult(req.ID, toolsCallResult{
		Content: []contentItem{{Type: "text", Text: string(data)}},
	})
}

func (s *Server) executeTool(name string, args map[string]any) (any, error) {
	if s.demo {
		return s.executeDemoTool(name, args)
	}

	server := stringArg(args, "server")

	// Route to remote if server is specified and not local
	if server != "" {
		srv := s.cfg.FindServer(server)
		if srv == nil {
			return nil, fmt.Errorf("server %q not found in config", server)
		}
		if !srv.Local {
			return s.executeRemote(srv, name, args)
		}
	}

	switch name {
	case "system_status":
		return system.Status()
	case "docker_list":
		return docker.List()
	case "docker_restart":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return docker.Restart(cname)
	case "docker_stop":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return docker.Stop(cname)
	case "docker_logs":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		lines := "50"
		if v := stringArg(args, "lines"); v != "" {
			lines = v
		}
		return docker.Logs(cname, lines)
	case "docker_stats":
		return docker.Stats()
	case "wake":
		target, ok := requireString(args, "target")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: target")
		}
		broadcast := "255.255.255.255"
		// Check if target is a name in config
		if wt := s.cfg.FindWakeTarget(target); wt != nil {
			target = wt.MAC
			if wt.Broadcast != "" {
				broadcast = wt.Broadcast
			}
		}
		if v := stringArg(args, "broadcast"); v != "" {
			broadcast = v
		}
		return wake.Send(target, broadcast)
	case "open_ports":
		return ports.List()
	case "network_scan":
		return network.ScanWithTimeout(30 * time.Second)
	case "alerts":
		return alerts.Check(&s.cfg.Alerts)
	case "inventory_scan":
		return inventory.Collect(s.cfg, inventory.DefaultCollectFuncs())
	case "inventory_export":
		format := stringArg(args, "format")
		if format == "" {
			format = "mermaid"
		}
		inv, err := inventory.Collect(s.cfg, inventory.DefaultCollectFuncs())
		if err != nil {
			return nil, err
		}
		switch format {
		case "mermaid":
			return map[string]any{"format": format, "content": inventory.RenderMermaid(inv)}, nil
		case "json":
			return inv, nil
		default:
			return nil, fmt.Errorf("unsupported format: %q (supported: mermaid, json)", format)
		}
	case "report":
		return report.Run(s.cfg, report.DefaultCollectFuncs(), report.Options{
			Keep:   intArg(args, "keep", 30),
			NoSave: boolArg(args, "no_save"),
		})
	case "backup_create":
		backupDir := stringArg(args, "to")
		if backupDir == "" {
			backupDir = s.cfg.ResolveBackupDir()
		}
		return backup.Run(backupDir, stringArg(args, "service"))
	case "backup_list":
		return backup.List(s.cfg.ResolveBackupDir())
	case "backup_drill":
		opts := backup.DrillOptions{
			BackupDir: s.cfg.ResolveBackupDir(),
			Archive:   stringArg(args, "archive"),
		}
		if boolArg(args, "all") {
			return backup.RunDrillAll(opts)
		}
		appName, ok := requireString(args, "app")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: app (or set all=true)")
		}
		return backup.RunDrill(appName, opts)
	case "backup_restore":
		archive, ok := requireString(args, "archive")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: archive")
		}
		return backup.Restore(archive, stringArg(args, "service"))

	case "install_list":
		return install.List(), nil

	case "install_app":
		appName := stringArg(args, "app")
		app, ok := install.Registry[appName]
		if !ok {
			return nil, fmt.Errorf("unknown app %q, use install_list to see available apps", appName)
		}
		opts := install.InstallOptions{Port: stringArg(args, "port")}
		port := app.DefaultPort
		if opts.Port != "" {
			port = opts.Port
		}
		issues := install.PreCheck(app, port)
		if len(issues) > 0 {
			return map[string]any{"status": "failed", "issues": issues}, nil
		}
		if err := install.Install(app, opts); err != nil {
			return nil, err
		}
		status, _ := install.Status(app.Name)
		return map[string]any{
			"status": "installed",
			"app":    app.Name,
			"port":   port,
			"path":   install.AppDir(app.Name),
			"state":  status,
		}, nil

	case "install_status":
		appName := stringArg(args, "app")
		status, err := install.Status(appName)
		if err != nil {
			return nil, err
		}
		return map[string]any{"app": appName, "state": status}, nil

	case "install_uninstall":
		appName := stringArg(args, "app")
		if err := install.Uninstall(appName); err != nil {
			return nil, err
		}
		return map[string]any{"status": "uninstalled", "app": appName, "data_preserved": true}, nil

	case "install_purge":
		appName := stringArg(args, "app")
		if err := install.Purge(appName); err != nil {
			return nil, err
		}
		return map[string]any{"status": "purged", "app": appName}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) executeRemote(srv *config.ServerConfig, tool string, args map[string]any) (any, error) {
	// Build remote command args
	var remoteArgs []string
	switch tool {
	case "system_status":
		remoteArgs = []string{"status", "--json"}
	case "docker_list":
		remoteArgs = []string{"docker", "list", "--json"}
	case "docker_restart":
		remoteArgs = []string{"docker", "restart", stringArg(args, "name"), "--json"}
	case "docker_stop":
		remoteArgs = []string{"docker", "stop", stringArg(args, "name"), "--json"}
	case "docker_logs":
		lines := "50"
		if v := stringArg(args, "lines"); v != "" {
			lines = v
		}
		remoteArgs = []string{"docker", "logs", stringArg(args, "name"), lines, "--json"}
	case "docker_stats":
		remoteArgs = []string{"docker", "stats", "--json"}
	case "open_ports":
		remoteArgs = []string{"ports", "--json"}
	case "alerts":
		remoteArgs = []string{"alerts", "--json"}
	case "inventory_scan":
		remoteArgs = []string{"inventory", "scan", "--json"}
	case "inventory_export":
		format := stringArg(args, "format")
		if format == "" {
			format = "mermaid"
		}
		if format == "json" {
			remoteArgs = []string{"inventory", "export", "--json"}
		} else {
			return nil, fmt.Errorf("remote inventory_export only supports format=json; use inventory_scan or run locally for Mermaid output")
		}
	case "report":
		remoteArgs = []string{"report", "--json", "--keep", strconv.Itoa(intArg(args, "keep", 30))}
		if boolArg(args, "no_save") {
			remoteArgs = append(remoteArgs, "--no-save")
		}
	case "backup_list":
		remoteArgs = []string{"backup", "list", "--json"}
	case "backup_create":
		remoteArgs = []string{"backup", "--json"}
		if service := stringArg(args, "service"); service != "" {
			remoteArgs = append(remoteArgs, "--service", service)
		}
		if to := stringArg(args, "to"); to != "" {
			remoteArgs = append(remoteArgs, "--to", to)
		}
	case "backup_drill":
		remoteArgs = []string{"backup", "drill", "--json"}
		if archive := stringArg(args, "archive"); archive != "" {
			remoteArgs = append(remoteArgs, "--archive", archive)
		}
		if boolArg(args, "all") {
			remoteArgs = append(remoteArgs, "--all")
		} else {
			appName, ok := requireString(args, "app")
			if !ok {
				return nil, fmt.Errorf("missing required parameter: app (or set all=true)")
			}
			remoteArgs = append(remoteArgs, appName)
		}
	case "backup_restore":
		archive, ok := requireString(args, "archive")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: archive")
		}
		remoteArgs = []string{"restore", archive, "--json"}
		if service := stringArg(args, "service"); service != "" {
			remoteArgs = append(remoteArgs, "--service", service)
		}
	default:
		return nil, fmt.Errorf("tool %q not supported for remote execution", tool)
	}

	out, err := remote.Run(srv, remoteArgs...)
	if err != nil {
		return nil, err
	}

	// Return raw JSON from remote as-is
	var result any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON from remote: %w", err)
	}
	return result, nil
}

func (s *Server) writeResult(id json.RawMessage, result any) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.out, "%s\n", data)
}

func (s *Server) writeError(id json.RawMessage, code int, message string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.out, "%s\n", data)
}

// Helper functions

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func requireString(args map[string]any, key string) (string, bool) {
	v := stringArg(args, key)
	return v, v != ""
}

func boolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		b, _ := strconv.ParseBool(val)
		return b
	default:
		return false
	}
}

func intArg(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	v, ok := args[key]
	if !ok {
		return fallback
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		i, err := strconv.Atoi(val)
		if err == nil {
			return i
		}
	}
	return fallback
}

func toolDefinitions() []toolDef {
	return []toolDef{
		{
			Name:        "system_status",
			Description: "Get system status including CPU, memory, disk usage, and uptime",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
			Name:        "docker_list",
			Description: "List Docker containers with their status, image, and ports",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
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
		{
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
		{
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
		{
			Name:        "docker_stats",
			Description: "Get resource usage statistics (CPU, memory, network, block I/O) for all running Docker containers",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
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
		{
			Name:        "open_ports",
			Description: "List open network ports with associated process information",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
			Name:        "network_scan",
			Description: "Scan the local network to discover devices (IP, MAC, hostname)",
			InputSchema: inputSchema{
				Type: "object",
			},
		},
		{
			Name:        "alerts",
			Description: "Check resource alerts for CPU, memory, and disk usage against configured thresholds",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
			Name:        "inventory_scan",
			Description: "Collect server inventory/topology including system status, Docker containers, app ports, and system ports",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
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
		{
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
		{
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
		{
			Name:        "backup_list",
			Description: "List existing backup archives in the configured backup directory",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
		{
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
		{
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
		{
			Name:        "install_list",
			Description: "List available self-hosted apps that can be installed",
			InputSchema: inputSchema{
				Type: "object",
			},
		},
		{
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
		{
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
		{
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
		{
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
	}
}
