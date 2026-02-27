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
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
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
	case "open_ports":
		remoteArgs = []string{"ports", "--json"}
	case "alerts":
		remoteArgs = []string{"alerts", "--json"}
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
	}
}
