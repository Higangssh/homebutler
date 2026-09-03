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
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/install"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/proxmox"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
	"github.com/Higangssh/homebutler/internal/watch"
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
	Data    any    `json:"data,omitempty"`
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

type discoverResult struct {
	ResultType        string       `json:"resultType"`
	SupportedVersions []string     `json:"supportedVersions"`
	Capabilities      capInfo      `json:"capabilities"`
	Meta              responseMeta `json:"_meta"`
}

type responseMeta struct {
	ServerInfo serverInfo `json:"io.modelcontextprotocol/serverInfo"`
}

type requestMeta struct {
	ProtocolVersion    string          `json:"io.modelcontextprotocol/protocolVersion"`
	ClientCapabilities json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`
}

// initializeParams is the part of the client's initialize request that this
// server acts on. Capabilities and clientInfo are read past deliberately: a
// tools-only server negotiates nothing that depends on them.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// supportedProtocolVersions lists the MCP revisions this server implements,
// newest first.
//
// homebutler answered every initialize with "2024-11-05" — the first revision
// ever published — and never read what the client asked for. That was
// conformant, because a server may answer with any version it supports, but it
// was the oldest thing it could conformantly say, and clients cap their
// behaviour to the version they are given.
//
// All five are listed because for a tools-only stdio server they describe the
// same surface. Everything the later revisions added is either out of scope
// here (resources, prompts, sampling, roots, elicitation, tasks, Streamable
// HTTP and its authorization) or already the behaviour: tool input validation
// errors come back as tool errors with IsError rather than as JSON-RPC errors
// (SEP-1303), and the generated inputSchema uses no construct outside JSON
// Schema 2020-12 (SEP-1613).
var supportedProtocolVersions = []string{
	"2026-07-28",
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// The two slices answer different questions and must not be used for each
// other's.
//
//   - supportedProtocolVersions is what this server can be reached by, both
//     eras, and is what server/discover advertises.
//   - modernProtocolVersions is what may appear in a request's _meta. By the
//     revision's own terminology a modern version is one that carries the
//     version as per-request metadata, so a legacy revision arriving there is
//     a contradiction rather than a version this server declined.
//
// Answering UnsupportedProtocolVersionError with the combined list told a
// client its version was unsupported and handed it a list containing that
// version, and the spec tells the client to pick from that list and retry.
var (
	modernProtocolVersions = supportedProtocolVersions[:1]
	legacyProtocolVersions = supportedProtocolVersions[1:]
)

// toolsListTTLMS is a freshness hint, not a promise that the tool registry is
// immutable. Change it if the registry becomes dynamic or a shorter polling
// interval is needed.
const toolsListTTLMS = 5 * 60 * 1000

// negotiateProtocolVersion answers with the version the client asked for when
// this server implements it, and with the newest one it does implement when it
// does not. That is what the lifecycle spec requires, and it is also the only
// shape that keeps working if a version is ever added or dropped here.
func negotiateProtocolVersion(requested string) string {
	for _, v := range legacyProtocolVersions {
		if v == requested {
			return requested
		}
	}
	return legacyProtocolVersions[0]
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
	ResultType string       `json:"resultType"`
	Tools      []toolDef    `json:"tools"`
	TTLMS      int          `json:"ttlMs"`
	CacheScope string       `json:"cacheScope"`
	Meta       responseMeta `json:"_meta"`
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
	ResultType string        `json:"resultType"`
	Content    []contentItem `json:"content"`
	IsError    bool          `json:"isError,omitempty"`
	Meta       responseMeta  `json:"_meta"`
}

// Server is the MCP server.
type Server struct {
	cfg     *config.Config
	cfgPath string
	version string
	demo    bool
	in      io.Reader
	out     io.Writer
}

// SetConfigPath records the --config path this server was started with.
//
// config_validate answers "is the config this server is running on valid", and
// without the path it would resolve one itself and check whatever the default
// rules select. That is a different file, correctly labelled in the result but
// not the one that was asked about.
func (s *Server) SetConfigPath(path string) { s.cfgPath = path }

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
	modern, err := validateRequestMeta(req)
	if err != nil {
		if req.ID != nil {
			if unsupported, ok := err.(*unsupportedProtocolError); ok {
				s.writeUnsupportedProtocolError(req.ID, unsupported.requested)
			} else {
				s.writeError(req.ID, -32602, err.Error())
			}
		}
		return
	}
	if modern && req.Method == "initialize" {
		if req.ID != nil {
			s.writeError(req.ID, -32601, "method not found: initialize")
		}
		return
	}

	switch req.Method {
	case "initialize":
		var params initializeParams
		// A request with no or unreadable params is not a reason to fail the
		// handshake; it just means there is nothing to echo, and the newest
		// supported version is the right answer.
		_ = json.Unmarshal(req.Params, &params)
		s.writeResult(req.ID, initializeResult{
			ProtocolVersion: negotiateProtocolVersion(params.ProtocolVersion),
			Capabilities:    capInfo{Tools: &toolsCap{}},
			ServerInfo:      serverInfo{Name: "homebutler", Version: s.version},
		})
	case "notifications/initialized":
		// Notification — no response needed
	case "ping":
		if modern {
			// ping was removed in 2026-07-28.
			if req.ID != nil {
				s.writeError(req.ID, -32601, "method not found: ping")
			}
			break
		}
		// Legacy clients expect the empty ping result.
		s.writeResult(req.ID, struct{}{})
	case "server/discover":
		s.writeResult(req.ID, discoverResult{
			ResultType:        "complete",
			SupportedVersions: supportedProtocolVersions,
			Capabilities:      capInfo{Tools: &toolsCap{}},
			Meta:              responseMeta{ServerInfo: serverInfo{Name: "homebutler", Version: s.version}},
		})
	case "tools/list":
		s.writeResult(req.ID, toolsListResult{
			ResultType: "complete",
			Tools:      toolDefinitions(),
			TTLMS:      toolsListTTLMS,
			CacheScope: "public",
			Meta:       responseMeta{ServerInfo: serverInfo{Name: "homebutler", Version: s.version}},
		})
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
			ResultType: "complete",
			Content:    []contentItem{{Type: "text", Text: toolErr.Error()}},
			IsError:    true,
			Meta:       responseMeta{ServerInfo: serverInfo{Name: "homebutler", Version: s.version}},
		})
		return
	}

	data, err := json.Marshal(result)
	if err != nil {
		s.writeResult(req.ID, toolsCallResult{
			ResultType: "complete",
			Content:    []contentItem{{Type: "text", Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError:    true,
			Meta:       responseMeta{ServerInfo: serverInfo{Name: "homebutler", Version: s.version}},
		})
		return
	}

	s.writeResult(req.ID, toolsCallResult{
		ResultType: "complete",
		Content:    []contentItem{{Type: "text", Text: string(data)}},
		Meta:       responseMeta{ServerInfo: serverInfo{Name: "homebutler", Version: s.version}},
	})
}

// containerArgTools lists the tools whose container name travels into a
// command line, here or on a remote host, and names the second value they
// forward alongside it where one exists. executeTool validates each of them
// before choosing a path, because the paths fail differently otherwise: the
// local switch lands in the docker package, which rejects a bad name with
// these exact words, while the remote path builds an argv for another
// homebutler whose own command parser reads a leading dash as a flag — the
// help text then comes back through remote.Run as if a restart had happened.
var containerArgTools = map[string]string{
	"docker_restart": "",
	"docker_stop":    "",
	"docker_logs":    "lines",
	"docker_top":     "",
	"docker_inspect": "",
}

// validateContainerArgs is the one gate for forwarded container arguments,
// applied before the local-or-remote decision so neither path can grow its
// own dialect of the rule. The error messages match what the docker package
// returns on the local path, so a caller cannot tell which machine would have
// answered from the rejection alone.
func validateContainerArgs(tool string, args map[string]any) error {
	numberArg, forwarded := containerArgTools[tool]
	if !forwarded {
		return nil
	}
	cname, ok := requireString(args, "name")
	if !ok {
		return fmt.Errorf("missing required parameter: name")
	}
	if !docker.ValidName(cname) {
		return fmt.Errorf("invalid container name: %s", cname)
	}
	if numberArg == "" {
		return nil
	}
	lines := "50"
	if v := stringArg(args, numberArg); v != "" {
		lines = v
	}
	if !docker.ValidLines(lines) {
		return fmt.Errorf("invalid line count: %s (must be a positive integer)", lines)
	}
	return nil
}

func (s *Server) executeTool(name string, args map[string]any) (any, error) {
	// Before any routing: whichever machine ends up answering, the argument
	// rules are the same and were already checked.
	if err := validateContainerArgs(name, args); err != nil {
		return nil, err
	}

	if s.demo {
		return s.executeDemoTool(name, args)
	}

	if cap, ok := capabilityFor(name); ok && cap.supports(targetProxmox) {
		if stringArg(args, "server") != "" {
			return nil, fmt.Errorf("tool %q cannot be pointed at a server; use endpoint", name)
		}
		return s.executeProxmox(name, args)
	}

	server := stringArg(args, "server")

	// Route to remote if server is specified and not local
	if server != "" {
		srv := s.cfg.FindServer(server)
		if srv == nil {
			return nil, fmt.Errorf("server %q not found in config", server)
		}
		if !srv.Local {
			// The registry decides what a tool can be pointed at. Before this,
			// the decision lived in executeRemote's switch default, so the
			// registry described a behaviour it did not control and the two
			// could disagree without any test noticing.
			cap, ok := capabilityFor(name)
			if !ok {
				return nil, fmt.Errorf("unknown tool: %s", name)
			}
			if !cap.supports(targetServer) {
				return nil, fmt.Errorf("tool %q cannot be pointed at a server", name)
			}
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
	case "docker_top":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return docker.Top(cname)
	case "docker_inspect":
		cname, ok := requireString(args, "name")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: name")
		}
		return docker.Inspect(cname)
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
	case "doctor":
		return doctor.Run(s.cfg, doctor.DefaultCollectFuncs(), doctor.Options{
			BackupMaxAge: time.Duration(intArg(args, "backup_max_age_hours", 168)) * time.Hour,
		})
	case "watch_check":
		dir, err := watch.WatchDir()
		if err != nil {
			return nil, err
		}
		return watch.CheckTargets(dir, s.resolveIncidentCap(dir))
	case "processes":
		return system.ListProcesses(intArg(args, "limit", 10), stringArg(args, "sort_by"))
	case "config_validate":
		// Deliberately not s.cfg: that is the already-loaded config, and
		// loading treats an unreadable or missing file as "use defaults",
		// which is exactly the failure this answers. Validate reads the file.
		result := config.Validate(s.cfgPath)
		passed := result.Errors() == 0
		if boolArg(args, "strict") && result.Warnings() > 0 {
			passed = false
		}
		// The result travels with the verdict rather than the verdict being an
		// error, so a caller that gates on passed still gets to see why.
		return map[string]any{
			"passed":   passed,
			"errors":   result.Errors(),
			"warnings": result.Warnings(),
			"result":   result,
		}, nil
	case "watch_history":
		dir, err := watch.WatchDir()
		if err != nil {
			return nil, err
		}
		return watch.History(dir, watch.HistoryOptions{
			Limit:     intArg(args, "limit", 10),
			Container: stringArg(args, "container"),
			Logs:      boolArg(args, "include_logs"),
		})
	case "watch_list":
		dir, err := watch.WatchDir()
		if err != nil {
			return nil, err
		}
		return watch.ListWatched(dir)
	case "backup_create":
		backupDir := stringArg(args, "to")
		if backupDir == "" {
			backupDir = s.cfg.ResolveBackupDir()
		}
		return backup.Run(backupDir, stringArg(args, "service"), s.cfg.ResolveBackupRetention())
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
		// No AllowBind: an agent has no way to name a host path it is
		// permitted to write to, so bind mounts declared by the archive are
		// always refused here and reported in the result.
		return backup.Restore(archive, backup.RestoreOptions{Service: stringArg(args, "service")})

	case "proxmox_script_list":
		return proxmox.Scripts(), nil

	case "proxmox_script_command":
		slug, ok := requireString(args, "slug")
		if !ok {
			return nil, fmt.Errorf("missing required parameter: slug")
		}
		command, err := proxmox.ScriptCommand(slug)
		if err != nil {
			return nil, err
		}
		return map[string]any{"slug": slug, "command": command, "warning": proxmox.ScriptWarning}, nil

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

// resolveIncidentCap resolves the incident retention cap the same way the
// watch commands do: config.yaml wins over watch/config.json, and an unset
// value takes the default rather than reading as unlimited.
//
// Without this, watch_check would prune on a different rule than `watch check`
// and `watch start`, and the incident history an agent reads would not match
// the one the terminal shows.
func (s *Server) resolveIncidentCap(dir string) int {
	watchCfg, err := watch.LoadWatchConfig(dir)
	if err != nil || watchCfg == nil {
		defaults := watch.DefaultWatchConfig()
		watchCfg = &defaults
	}
	if s.cfg != nil {
		watchCfg.Retention = s.cfg.Watch.Retention
	}
	watchCfg.Retention.Normalize()
	return watchCfg.Retention.MaxIncidents
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
	case "docker_top":
		remoteArgs = []string{"docker", "top", stringArg(args, "name"), "--json"}
	case "docker_inspect":
		remoteArgs = []string{"docker", "inspect", stringArg(args, "name"), "--json"}
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
	case "doctor":
		remoteArgs = []string{"doctor", "--json", "--backup-max-age", fmt.Sprintf("%dh", intArg(args, "backup_max_age_hours", 168))}
	case "watch_check":
		remoteArgs = []string{"watch", "check", "--json"}
	case "processes":
		remoteArgs = []string{"processes", "--json",
			"--limit", strconv.Itoa(intArg(args, "limit", 10))}
		if by := stringArg(args, "sort_by"); by != "" {
			remoteArgs = append(remoteArgs, "--sort", by)
		}
	case "watch_history":
		// The flags go over rather than being applied to the response, so the
		// remote answer is the same shape the local one is and the logs are
		// left on the remote host unless they were asked for.
		remoteArgs = []string{"watch", "history", "--json",
			"--limit", strconv.Itoa(intArg(args, "limit", 10))}
		if c := stringArg(args, "container"); c != "" {
			remoteArgs = append(remoteArgs, "--container", c)
		}
		if boolArg(args, "include_logs") {
			remoteArgs = append(remoteArgs, "--logs")
		}
	case "watch_list":
		remoteArgs = []string{"watch", "list", "--json"}
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
		// Unreachable: executeTool checks the registry before routing here.
		// Kept so a tool added to the registry with targetServer but no argv
		// mapping fails loudly instead of running an empty remote command.
		return nil, fmt.Errorf("tool %q has no remote command mapping", tool)
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

func (s *Server) writeUnsupportedProtocolError(id json.RawMessage, requested string) {
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code:    -32022,
		Message: "Unsupported protocol version",
		Data:    map[string]any{"supported": modernProtocolVersions, "requested": requested},
	}}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.out, "%s\n", data)
}

// validateRequestMeta identifies and validates the stateless modern request
// envelope. An absent envelope is deliberately accepted for legacy clients.
func validateRequestMeta(req *jsonRPCRequest) (bool, error) {
	if len(req.Params) == 0 {
		return false, nil
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(req.Params, &params); err != nil || params == nil {
		return false, fmt.Errorf("invalid params")
	}
	rawMeta, present := params["_meta"]
	if !present {
		return false, nil
	}
	var meta requestMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil || meta.ProtocolVersion == "" || len(meta.ClientCapabilities) == 0 {
		return true, fmt.Errorf("invalid request metadata")
	}
	var capabilities map[string]any
	if err := json.Unmarshal(meta.ClientCapabilities, &capabilities); err != nil || capabilities == nil {
		return true, fmt.Errorf("invalid client capabilities")
	}
	if !isModernProtocolVersion(meta.ProtocolVersion) {
		return true, &unsupportedProtocolError{requested: meta.ProtocolVersion}
	}
	return true, nil
}

type unsupportedProtocolError struct{ requested string }

func (e *unsupportedProtocolError) Error() string { return "Unsupported protocol version" }

func isModernProtocolVersion(version string) bool {
	for _, modern := range modernProtocolVersions {
		if version == modern {
			return true
		}
	}
	return false
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
