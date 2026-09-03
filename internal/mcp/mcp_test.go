package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

func newTestServer() (*Server, *bytes.Buffer) {
	cfg := &config.Config{
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
		Wake: []config.WakeTarget{
			{Name: "nas", MAC: "AA:BB:CC:DD:EE:FF", Broadcast: "192.168.1.255"},
		},
	}
	out := &bytes.Buffer{}
	s := NewServer(cfg, "test")
	s.out = out
	return s, out
}

func sendAndReceive(t *testing.T, s *Server, out *bytes.Buffer, request string) jsonRPCResponse {
	t.Helper()
	s.in = strings.NewReader(request + "\n")
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response %q: %v", out.String(), err)
	}
	return resp
}

func TestInitialize(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var init initializeResult
	if err := json.Unmarshal(result, &init); err != nil {
		t.Fatalf("unmarshal initializeResult: %v", err)
	}

	// params carried no protocolVersion, so there is nothing to echo and the
	// newest supported revision is the right answer.
	if init.ProtocolVersion != legacyProtocolVersions[0] {
		t.Errorf("protocolVersion = %q, want %q", init.ProtocolVersion, legacyProtocolVersions[0])
	}
	if init.ServerInfo.Name != "homebutler" {
		t.Errorf("serverInfo.name = %q, want %q", init.ServerInfo.Name, "homebutler")
	}
	if init.Capabilities.Tools == nil {
		t.Error("capabilities.tools should not be nil")
	}
}

func TestNotificationsInitialized(t *testing.T) {
	s, out := newTestServer()
	// Notification has no id, should produce no response
	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	s.in = strings.NewReader(req + "\n")
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for notification, got: %s", out.String())
	}
}

func TestToolsList(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var list toolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatalf("unmarshal toolsListResult: %v", err)
	}

	// Derived from the registry rather than repeated here. The count and the
	// name set were hardcoded, so adding a tool meant a failure in a test that
	// had nothing to say about the change — it only knew the list had moved.
	// What is worth asserting is that tools/list reports exactly what the
	// registry holds, which is the thing that could actually be wrong.
	if len(list.Tools) != len(capabilityRegistry) {
		t.Errorf("tools/list returned %d tools, registry holds %d", len(list.Tools), len(capabilityRegistry))
	}

	expectedTools := make(map[string]bool, len(capabilityRegistry))
	for _, c := range capabilityRegistry {
		expectedTools[c.tool.Name] = false
	}

	for _, tool := range list.Tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %s inputSchema.type = %q, want %q", tool.Name, tool.InputSchema.Type, "object")
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found", name)
		}
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var callResult toolsCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		t.Fatalf("unmarshal toolsCallResult: %v", err)
	}

	if !callResult.IsError {
		t.Error("expected isError=true for unknown tool")
	}
	if len(callResult.Content) == 0 || !strings.Contains(callResult.Content[0].Text, "unknown tool") {
		t.Error("expected error message about unknown tool")
	}
}

func TestToolsCallMissingRequired(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"docker_restart","arguments":{}}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var callResult toolsCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		t.Fatalf("unmarshal toolsCallResult: %v", err)
	}

	if !callResult.IsError {
		t.Error("expected isError=true for missing required param")
	}
	if len(callResult.Content) == 0 || !strings.Contains(callResult.Content[0].Text, "missing required") {
		t.Error("expected error message about missing required parameter")
	}
}

func TestUnknownMethod(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":5,"method":"unknown/method","params":{}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32601)
	}
}

func TestInvalidJSON(t *testing.T) {
	s, out := newTestServer()
	s.in = strings.NewReader("not json\n")
	out.Reset()
	_ = s.Run()

	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32700)
	}
}

func TestToolsCallRemoteServerNotFound(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"system_status","arguments":{"server":"nonexistent"}}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var callResult toolsCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		t.Fatalf("unmarshal toolsCallResult: %v", err)
	}

	if !callResult.IsError {
		t.Error("expected isError=true for unknown server")
	}
	if len(callResult.Content) == 0 || !strings.Contains(callResult.Content[0].Text, "not found") {
		t.Error("expected error message about server not found")
	}
}

func TestMultipleRequests(t *testing.T) {
	s, out := newTestServer()
	lines := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}, "\n")
	s.in = strings.NewReader(lines + "\n")
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Should have exactly 2 responses (initialize and tools/list — notification produces none)
	outputLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(outputLines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %s", len(outputLines), out.String())
	}

	// First response: initialize
	var resp1 jsonRPCResponse
	if err := json.Unmarshal([]byte(outputLines[0]), &resp1); err != nil {
		t.Fatalf("parse response 1: %v", err)
	}
	if resp1.Error != nil {
		t.Errorf("response 1 unexpected error: %v", resp1.Error)
	}

	// Second response: tools/list
	var resp2 jsonRPCResponse
	if err := json.Unmarshal([]byte(outputLines[1]), &resp2); err != nil {
		t.Fatalf("parse response 2: %v", err)
	}
	if resp2.Error != nil {
		t.Errorf("response 2 unexpected error: %v", resp2.Error)
	}
}

func TestEmptyLines(t *testing.T) {
	s, out := newTestServer()
	input := "\n\n" + `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n\n"
	s.in = strings.NewReader(input)
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	outputLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(outputLines) != 1 {
		t.Fatalf("expected 1 response line, got %d", len(outputLines))
	}
}

func TestToolDefinitionsHaveRequiredFields(t *testing.T) {
	tools := toolDefinitions()
	requireMap := map[string][]string{
		"docker_restart":         {"name"},
		"docker_stop":            {"name"},
		"docker_logs":            {"name"},
		"docker_top":             {"name"},
		"docker_inspect":         {"name"},
		"wake":                   {"target"},
		"install_app":            {"app"},
		"install_status":         {"app"},
		"install_uninstall":      {"app"},
		"install_purge":          {"app"},
		"backup_restore":         {"archive"},
		"proxmox_node":           {"node"},
		"proxmox_tasks":          {"node"},
		"proxmox_guest_start":    {"endpoint", "node", "type", "vmid", "confirm"},
		"proxmox_guest_reboot":   {"endpoint", "node", "type", "vmid", "confirm"},
		"proxmox_guest_shutdown": {"endpoint", "node", "type", "vmid", "confirm"},
		"proxmox_task_status":    {"endpoint", "node", "upid"},
		"proxmox_script_command": {"slug"},
	}

	for _, tool := range tools {
		expected, hasRequired := requireMap[tool.Name]
		if hasRequired {
			if len(tool.InputSchema.Required) != len(expected) {
				t.Errorf("tool %s: expected %d required fields, got %d", tool.Name, len(expected), len(tool.InputSchema.Required))
			}
			for _, req := range expected {
				found := false
				for _, r := range tool.InputSchema.Required {
					if r == req {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("tool %s: missing required field %q", tool.Name, req)
				}
			}
		} else {
			if len(tool.InputSchema.Required) != 0 {
				t.Errorf("tool %s: expected no required fields, got %v", tool.Name, tool.InputSchema.Required)
			}
		}
	}
}

func TestCapabilityRegistryMetadata(t *testing.T) {
	if len(capabilityRegistry) != len(toolDefinitions()) {
		t.Fatalf("capability registry count mismatch: registry=%d tools=%d", len(capabilityRegistry), len(toolDefinitions()))
	}

	names := make(map[string]bool)
	for _, c := range capabilityRegistry {
		if c.tool.Name == "" {
			t.Fatal("capability has empty tool name")
		}
		if names[c.tool.Name] {
			t.Fatalf("duplicate capability for tool %q", c.tool.Name)
		}
		names[c.tool.Name] = true
		if c.risk == "" {
			t.Fatalf("tool %q has empty risk", c.tool.Name)
		}
		switch c.risk {
		case riskRead, riskWrite, riskDestructive:
		default:
			t.Fatalf("tool %q has unknown risk %q", c.tool.Name, c.risk)
		}
	}
}

func TestStringArg(t *testing.T) {
	args := map[string]any{
		"str":   "hello",
		"num":   float64(42),
		"float": float64(3.14),
		"bool":  true,
	}

	if v := stringArg(args, "str"); v != "hello" {
		t.Errorf("stringArg(str) = %q, want %q", v, "hello")
	}
	if v := stringArg(args, "num"); v != "42" {
		t.Errorf("stringArg(num) = %q, want %q", v, "42")
	}
	if v := stringArg(args, "missing"); v != "" {
		t.Errorf("stringArg(missing) = %q, want empty", v)
	}
	if v := stringArg(nil, "key"); v != "" {
		t.Errorf("stringArg(nil, key) = %q, want empty", v)
	}
	if v := stringArg(args, "bool"); v != "true" {
		t.Errorf("stringArg(bool) = %q, want %q", v, "true")
	}
}

func TestRequireString(t *testing.T) {
	tests := []struct {
		name   string
		args   map[string]any
		key    string
		wantV  string
		wantOK bool
	}{
		{"present", map[string]any{"name": "foo"}, "name", "foo", true},
		{"missing", map[string]any{}, "name", "", false},
		{"empty-value", map[string]any{"name": ""}, "name", "", false},
		{"nil-args", nil, "name", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := requireString(tt.args, tt.key)
			if v != tt.wantV {
				t.Errorf("requireString() v = %q, want %q", v, tt.wantV)
			}
			if ok != tt.wantOK {
				t.Errorf("requireString() ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestHandleToolCallInvalidParams(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":"not-an-object"}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32602)
	}
}

func TestDemoToolsWithServerVariants(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)

	servers := []string{"", "nas-box", "raspberry-pi", "homelab-server"}
	tools := []string{"system_status", "docker_list", "open_ports", "alerts"}

	for _, srv := range servers {
		for _, tool := range tools {
			t.Run(tool+"/"+srv, func(t *testing.T) {
				args := map[string]any{}
				if srv != "" {
					args["server"] = srv
				}
				res, err := s.executeDemoTool(tool, args)
				if err != nil {
					t.Fatalf("executeDemoTool(%q) error: %v", tool, err)
				}
				if res == nil {
					t.Fatalf("executeDemoTool(%q) returned nil", tool)
				}
			})
		}
	}
}

func TestDemoToolsWithArgs(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)

	tests := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"docker-restart", "docker_restart", map[string]any{"name": "nginx"}},
		{"docker-stop", "docker_stop", map[string]any{"name": "nginx"}},
		{"docker-logs-known", "docker_logs", map[string]any{"name": "postgres"}},
		{"docker-logs-backup", "docker_logs", map[string]any{"name": "backup"}},
		{"docker-logs-unknown", "docker_logs", map[string]any{"name": "unknown-container"}},
		{"wake", "wake", map[string]any{"target": "AA:BB:CC:DD:EE:FF"}},
		{"network-scan", "network_scan", map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := s.executeDemoTool(tt.tool, tt.args)
			if err != nil {
				t.Fatalf("executeDemoTool(%q) error: %v", tt.tool, err)
			}
			if res == nil {
				t.Fatalf("executeDemoTool(%q) returned nil", tt.tool)
			}
		})
	}
}

func TestInitializeVersion(t *testing.T) {
	cfg := &config.Config{}
	out := &bytes.Buffer{}
	s := NewServer(cfg, "1.2.3")
	s.out = out

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	s.in = strings.NewReader(req + "\n")
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	var resp jsonRPCResponse
	json.Unmarshal(out.Bytes(), &resp)
	result, _ := json.Marshal(resp.Result)
	var init initializeResult
	json.Unmarshal(result, &init)

	if init.ServerInfo.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", init.ServerInfo.Version, "1.2.3")
	}
}

// homebutler answered every initialize with 2024-11-05 — the first MCP revision
// ever published — and never read what the client asked for. That was
// conformant, because a server may answer with any version it supports, but it
// was the oldest thing it could conformantly say, and clients cap their
// behaviour to the version they are handed.
func TestNegotiateProtocolVersion(t *testing.T) {
	latest := legacyProtocolVersions[0]

	// "If the server supports the requested protocol version, it MUST respond
	// with the same version."
	for _, v := range legacyProtocolVersions {
		if got := negotiateProtocolVersion(v); got != v {
			t.Errorf("negotiateProtocolVersion(%q) = %q, want the same version back", v, got)
		}
	}

	// "Otherwise, the server MUST respond with another protocol version it
	// supports. This SHOULD be the latest version supported by the server."
	for _, v := range []string{"1.0.0", "2099-01-01", ""} {
		if got := negotiateProtocolVersion(v); got != latest {
			t.Errorf("negotiateProtocolVersion(%q) = %q, want the latest supported %q", v, got, latest)
		}
	}

	if latest == "2024-11-05" {
		t.Error("the newest supported revision is still the first one ever published")
	}
}

func TestInitializeEchoesTheRequestedVersion(t *testing.T) {
	for _, want := range legacyProtocolVersions {
		s, out := newTestServer()
		req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + want + `"}}`
		resp := sendAndReceive(t, s, out, req)
		if resp.Error != nil {
			t.Fatalf("%s: unexpected error: %v", want, resp.Error)
		}

		result, _ := json.Marshal(resp.Result)
		var init initializeResult
		if err := json.Unmarshal(result, &init); err != nil {
			t.Fatalf("%s: unmarshal: %v", want, err)
		}
		if init.ProtocolVersion != want {
			t.Errorf("client asked for %q, server answered %q", want, init.ProtocolVersion)
		}
	}
}

func TestInitializeAnswersLatestForAVersionItDoesNotSpeak(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1.0.0"}}`
	resp := sendAndReceive(t, s, out, req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var init initializeResult
	if err := json.Unmarshal(result, &init); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if init.ProtocolVersion != legacyProtocolVersions[0] {
		t.Errorf("protocolVersion = %q, want the latest legacy version %q", init.ProtocolVersion, legacyProtocolVersions[0])
	}
}

// A client that opens with no params at all still gets a handshake rather than
// a parse failure.
func TestInitializeWithoutParams(t *testing.T) {
	s, out := newTestServer()
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	var init initializeResult
	if err := json.Unmarshal(result, &init); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if init.ProtocolVersion != legacyProtocolVersions[0] {
		t.Errorf("protocolVersion = %q", init.ProtocolVersion)
	}
}

// "The receiver MUST respond promptly with an empty response." Sending a ping
// is optional; answering one is not, and this came back as -32601 method not
// found, which a client is entitled to read as a dead connection.
func TestPingIsAnswered(t *testing.T) {
	s, out := newTestServer()
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)

	if resp.Error != nil {
		t.Fatalf("ping returned an error: %v", resp.Error)
	}
	result, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(result) != "{}" {
		t.Errorf("legacy ping result = %s, want an empty object", result)
	}
}

func modernMeta(version string) string {
	return `"_meta":{"io.modelcontextprotocol/protocolVersion":"` + version + `","io.modelcontextprotocol/clientCapabilities":{}}`
}

func TestModernDiscover(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + modernMeta(supportedProtocolVersions[0]) + `}}`
	resp := sendAndReceive(t, s, out, req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	var discover discoverResult
	if err := json.Unmarshal(result, &discover); err != nil {
		t.Fatalf("unmarshal discover result: %v", err)
	}
	if discover.ResultType != "complete" {
		t.Errorf("resultType = %q, want complete", discover.ResultType)
	}
	if len(discover.SupportedVersions) == 0 || discover.SupportedVersions[0] != "2026-07-28" {
		t.Errorf("supportedVersions = %v", discover.SupportedVersions)
	}
	if discover.Meta.ServerInfo.Name != "homebutler" || discover.Meta.ServerInfo.Version != "test" {
		t.Errorf("serverInfo = %+v", discover.Meta.ServerInfo)
	}
}

func TestDiscoverProbeWorksWithoutMetadata(t *testing.T) {
	s, out := newTestServer()
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	var discover discoverResult
	if err := json.Unmarshal(result, &discover); err != nil {
		t.Fatalf("unmarshal discover result: %v", err)
	}
	if discover.ResultType != "complete" || len(discover.SupportedVersions) == 0 {
		t.Errorf("discover result = %+v", discover)
	}
}

func TestLegacyVersionInModernMetadataIsRejected(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + modernMeta(legacyProtocolVersions[0]) + `}}`
	resp := sendAndReceive(t, s, out, req)
	if resp.Error == nil || resp.Error.Code != -32022 {
		t.Fatalf("error = %+v, want -32022", resp.Error)
	}
}

func TestModernUnsupportedVersion(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + modernMeta("1900-01-01") + `}}`
	resp := sendAndReceive(t, s, out, req)
	if resp.Error == nil || resp.Error.Code != -32022 {
		t.Fatalf("error = %+v, want -32022", resp.Error)
	}
	data, ok := resp.Error.Data.(map[string]any)
	if !ok {
		t.Fatalf("error data = %#v, want object", resp.Error.Data)
	}
	if data["requested"] != "1900-01-01" {
		t.Errorf("requested = %#v", data["requested"])
	}
}

func TestRemovedPingIsRejectedByModernProtocol(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{` + modernMeta(supportedProtocolVersions[0]) + `}}`
	resp := sendAndReceive(t, s, out, req)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v, want method not found", resp.Error)
	}
}

func TestModernInitializeIsRejected(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25",` + modernMeta(supportedProtocolVersions[0]) + `}}`
	resp := sendAndReceive(t, s, out, req)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v, want method not found", resp.Error)
	}
}

func TestToolsListIsCacheableAndDeterministic(t *testing.T) {
	s, out := newTestServer()
	modern := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + modernMeta(supportedProtocolVersions[0]) + `}}`
	first := sendAndReceive(t, s, out, modern)
	firstJSON, _ := json.Marshal(first.Result)
	second := sendAndReceive(t, s, out, modern)
	secondJSON, _ := json.Marshal(second.Result)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("tools/list order or result changed between identical requests")
	}
	var list toolsListResult
	if err := json.Unmarshal(firstJSON, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}
	if list.ResultType != "complete" || list.TTLMS <= 0 || list.CacheScope != "public" {
		t.Errorf("cacheable result = %+v", list)
	}
	for i := 1; i < len(list.Tools); i++ {
		if list.Tools[i-1].Name >= list.Tools[i].Name {
			t.Fatalf("tools are not sorted: %q before %q", list.Tools[i-1].Name, list.Tools[i].Name)
		}
	}
}

// The error told a client its version was unsupported and handed back a list
// containing that version. The spec tells the client to pick from the list and
// retry, so an obedient client loops.
func TestUnsupportedVersionErrorListsOnlyWhatMetaAccepts(t *testing.T) {
	s, out := newTestServer()
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{`+
		`"io.modelcontextprotocol/protocolVersion":"2024-11-05",`+
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`)

	if resp.Error == nil || resp.Error.Code != -32022 {
		t.Fatalf("expected -32022, got %+v", resp.Error)
	}
	data, _ := json.Marshal(resp.Error.Data)
	var payload struct {
		Supported []string `json:"supported"`
		Requested string   `json:"requested"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal error data: %v", err)
	}
	if len(payload.Supported) == 0 {
		t.Fatal("the supported list is empty")
	}
	for _, v := range payload.Supported {
		if v == payload.Requested {
			t.Errorf("the refused version %q is offered back in %v", v, payload.Supported)
		}
		for _, legacy := range legacyProtocolVersions {
			if v == legacy {
				t.Errorf("legacy version %q is offered as usable in _meta", v)
			}
		}
	}
}

// server/discover still advertises both eras: a dual-era server genuinely can
// be reached by an initialize handshake, and the spec's own example mixes them.
func TestDiscoverStillAdvertisesBothEras(t *testing.T) {
	s, out := newTestServer()
	sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`)
	body := out.String()
	if !strings.Contains(body, "2026-07-28") || !strings.Contains(body, "2024-11-05") {
		t.Errorf("discover no longer advertises both eras: %s", body)
	}
}
