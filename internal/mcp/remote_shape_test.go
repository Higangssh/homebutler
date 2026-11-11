package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/util"
)

// remoteRecording is what a remote homebutler printed for the command a tool
// forwards, and the arguments the tool was called with.
type remoteRecording struct {
	release string
	args    map[string]any
	out     string
}

// remoteRecordings holds, for every tool that can be pointed at a server,
// what the remote CLI prints. The ones marked 1.5.0 were recorded from a
// Raspberry Pi running it, with names shortened; the rest are what the
// command's --json prints, written from the type it passes to output().
//
// A tool added with capability.TargetServer has to be added here, so whoever
// adds it has to say what the remote prints and see it checked against the
// local answer.
var remoteRecordings = map[string][]remoteRecording{
	"system_status":  {{release: "1.5.0", out: `{"schema_version":1,"hostname":"pi","os":"linux","arch":"arm64","uptime":"2d 17h","cpu":{"usage_percent":0,"cores":4},"memory":{"total_gb":7.75,"used_gb":0.42,"usage_percent":5.44},"disks":[{"mount":"/","total_gb":59,"used_gb":10,"usage_percent":18}],"time":"2026-11-11T17:09:32+09:00"}`}},
	"docker_list":    {{release: "1.5.0", out: `[{"id":"8911e27328b4","name":"registry","image":"registry:2","status":"Stopped · 2d ago","state":"exited","ports":""}]`}},
	"docker_stats":   {{release: "1.5.0", out: `[]`}, {out: `[{"id":"abc","name":"web","cpu_percent":"0.10%","mem_usage":"10MiB / 1GiB","mem_percent":"1.00%","net_io":"1kB / 2kB","block_io":"0B / 0B","pids":"3"}]`}},
	"docker_restart": {{args: map[string]any{"name": "web"}, out: `{"action":"restart","container":"web","status":"ok"}`}},
	"docker_stop":    {{args: map[string]any{"name": "web"}, out: `{"action":"stop","container":"web","status":"ok"}`}},
	"docker_logs":    {{args: map[string]any{"name": "web"}, out: `{"container":"web","lines":"50","logs":"ready\n"}`}},
	"docker_top":     {{args: map[string]any{"name": "web"}, out: `{"container":"web","processes":[{"pid":"1","user":"root","command":"nginx"}]}`}},
	"docker_inspect": {{args: map[string]any{"name": "web"}, out: `{"name":"web","image":"nginx:1","status":"running","restart_policy":"always","restart_count":0,"ports":[],"mounts":[],"networks":[]}`}},
	// The bare list, in every release from v0.1.0 to v1.7.0.
	"open_ports": {
		{release: "1.5.0", out: `[{"protocol":"tcp","address":"0.0.0.0","port":"22"},{"protocol":"tcp","address":"[::]","port":"22"}]`},
		{release: "1.5.0", out: `[]`},
		{out: `{"ports":[{"protocol":"tcp","address":"0.0.0.0","port":"22"}]}`},
	},
	"alerts":         {{release: "1.5.0", out: `{"cpu":{"status":"ok","current":0,"threshold":90},"memory":{"status":"ok","current":5.46,"threshold":85},"disks":[{"mount":"/","status":"ok","current":18,"threshold":90}]}`}},
	"alerts_history": {{release: "1.5.0", out: `[]`}},
	"inventory_scan": {{release: "1.5.0", out: `{"server_name":"pi","host":"192.0.2.4","system":{"hostname":"pi","os":"linux","arch":"arm64","uptime":"2d","cpu":{"usage_percent":0.24,"cores":4},"memory":{"total_gb":7.75,"used_gb":0.42,"usage_percent":5.43},"disks":[],"time":"2026-11-11T17:09:39+09:00"},"containers":[],"ports":[{"protocol":"tcp","address":"0.0.0.0","port":"22"}]}`}},
	// format=json is the only one a server takes, and it answers the inventory
	// itself locally too; see inventoryExportJSON below.
	"inventory_export": {{args: map[string]any{"format": "json"}, out: `{"server_name":"pi","host":"192.0.2.4","containers":[],"ports":[]}`}},
	"report":           {{release: "1.5.0", args: map[string]any{"no_save": true}, out: `{"schema_version":1,"timestamp":"2026-11-11T08:09:42Z","server_name":"pi","is_baseline":true,"snapshot_saved":false,"running_count":0,"stopped_count":1,"public_port_count":2,"status":["Containers: 0 running, 1 stopped"],"needs_attention":[{"kind":"container","text":"1 container(s) stopped"}],"notable_changes":[],"suggested_actions":[]}`}},
	"doctor":           {{release: "1.5.0", out: `{"schema_version":1,"timestamp":"2026-11-11T08:09:39Z","server_name":"pi","status":"warn","summary":{"pass":0,"warn":1,"fail":0},"findings":[{"severity":"warn","category":"docker","title":"1 container(s) are stopped","detail":"registry","action":"Check the logs.","command":"homebutler docker logs registry","runner":"mcp","tool":"docker_logs"}]}`}},
	"processes":        {{release: "1.5.0", out: `{"processes":[{"pid":37511,"name":"sshd","cpu":7.1,"mem":0.1,"rss":8936,"state":"S"}],"total":42}`}},
	// A sentence, under --json, through 1.7.0, when nothing is watched.
	"watch_check": {
		{release: "1.5.0", out: "No targets being watched.\n"},
		{out: `{"checked":0}`},
		{out: `{"checked":1,"skipped":[{"name":"api","kind":"pm2"}]}`},
	},
	"watch_history": {{release: "1.5.0", out: `[]`}},
	"watch_list":    {{release: "1.5.0", out: `[]`}},
	// Sentences, under --json, through 1.7.0.
	"watch_add": {
		{release: "1.7.0", args: map[string]any{"container": "web"}, out: "Added docker \"web\" to watch list.\n  The running service reads the watch list at startup:\n    systemctl --user restart homebutler-watch\n"},
		{release: "1.7.0", args: map[string]any{"container": "web", "kind": "pm2"}, out: "pm2 \"web\" is already being watched.\n"},
		{args: map[string]any{"container": "web"}, out: `{"container":"web","kind":"docker","added":true}`},
	},
	"watch_remove": {
		{release: "1.7.0", args: map[string]any{"container": "web"}, out: "Removed \"web\" from watch list.\n"},
		{args: map[string]any{"container": "web"}, out: `{"container":"web","removed":true}`},
	},
	"notify_test":    {{out: `[{"channel":"telegram","sent":true}]`}},
	"backup_create":  {{out: `{"archive":"/b/backup.tar.gz","services":["web"],"size":"1.0 MB","volumes":1}`}},
	"backup_list":    {{release: "1.5.0", out: `[]`}},
	"backup_drill":   {{args: map[string]any{"app": "web"}, out: `{"app":"web","archive":"/b/backup.tar.gz","passed":true}`}},
	"backup_restore": {{args: map[string]any{"archive": "/b/backup.tar.gz"}, out: `{"archive":"/b/backup.tar.gz","services":["web"],"volumes":1}`}},
}

// answerType is the type a tool's local answer has, as ToolOutputs records it.
func answerType(t *testing.T, tool string, args map[string]any) reflect.Type {
	t.Helper()
	if tool == "inventory_export" && args["format"] == "json" {
		// InventoryExportResult is the mermaid answer; json answers the
		// inventory itself, here and over SSH alike.
		return reflect.TypeOf(inventory.Inventory{})
	}
	answer, ok := ToolOutputs[tool]
	if !ok {
		t.Fatalf("%s is not in ToolOutputs", tool)
	}
	return reflect.TypeOf(answer)
}

// strictlyIs decodes data into a new value of typ and fails on any field typ
// does not have, so a remote answer in another shape, or still wrapped in
// its CLI envelope, cannot pass for the local one.
func strictlyIs(typ reflect.Type, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(reflect.New(typ).Interface())
}

func answeringRemote(t *testing.T, out string, sent *[]string) {
	t.Helper()
	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	runRemote = func(_ *config.ServerConfig, args ...string) ([]byte, error) {
		*sent = args
		return []byte(out), nil
	}
}

func remoteServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newTestServer()
	s.cfg.Servers = []config.ServerConfig{{Name: "pi", Host: "192.0.2.4"}}
	return s
}

// Naming a server is not a way to get another shape. docs/compatibility.md
// freezes what every MCP tool answers with, and open_ports answered
// {"ports": [...]} here and the bare list for a server since the first MCP
// release. This walks every tool that takes a server, so the next one fails
// here instead of in an agent.
func TestEveryRemoteAnswerHasTheLocalShape(t *testing.T) {
	for _, def := range capability.Definitions() {
		c, _ := capability.For(def.Name)
		if !c.Supports(capability.TargetServer) {
			if _, ok := remoteRecordings[def.Name]; ok {
				t.Errorf("%s cannot be pointed at a server but has remote recordings", def.Name)
			}
			continue
		}
		recordings, ok := remoteRecordings[def.Name]
		if !ok {
			t.Errorf("%s can be pointed at a server; add what the remote prints to remoteRecordings", def.Name)
			continue
		}
		for _, rec := range recordings {
			args := map[string]any{"server": "pi"}
			for k, v := range rec.args {
				args[k] = v
			}
			var sent []string
			answeringRemote(t, rec.out, &sent)

			result, err := remoteServer(t).executeTool(def.Name, args)
			if err != nil {
				t.Errorf("%s from %q: %v", def.Name, rec.out, err)
				continue
			}
			if len(sent) == 0 {
				t.Errorf("%s did not reach the remote", def.Name)
			}
			data, err := json.Marshal(util.EmptyLists(result))
			if err != nil {
				t.Fatal(err)
			}
			typ := answerType(t, def.Name, rec.args)
			if err := strictlyIs(typ, data); err != nil {
				t.Errorf("%s over SSH (release %q) answers %s, which is not %s: %v", def.Name, rec.release, data, typ, err)
			}
			if isList := typ.Kind() == reflect.Slice; isList != bytes.HasPrefix(data, []byte("[")) {
				t.Errorf("%s over SSH answers %s; the local answer is a %s", def.Name, data, typ)
			}
		}
	}
}

// What these turn into is more than a shape: a sentence read as the wrong
// outcome would be a result an agent acts on.
func TestOlderRemoteSentencesBecomeTheRightOutcome(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		out  string
		want string
	}{
		{"open_ports", nil, `[]`, `{"ports":[]}`},
		{"open_ports", nil, `{"ports":null}`, `{"ports":[]}`},
		{"watch_check", nil, "No targets being watched.\n", `{"checked":0}`},
		{"watch_check", nil, "No containers being watched.\n", `{"checked":0}`},
		// Lists are never null through MCP, and ProcessList already reads
		// null as the empty list; the answer has to carry that one.
		{"processes", nil, `{"processes":null,"total":0}`, `{"processes":[],"total":0}`},
		{"watch_add", map[string]any{"container": "web"}, "Added docker \"web\" to watch list.\n", `{"container":"web","kind":"docker","added":true}`},
		{"watch_add", map[string]any{"container": "web", "kind": "pm2"}, "pm2 \"web\" is already being watched.\n", `{"container":"web","kind":"pm2","added":false}`},
		{"watch_remove", map[string]any{"container": "web"}, "Removed \"web\" from watch list.\n", `{"container":"web","removed":true}`},
	}
	for _, tc := range cases {
		args := map[string]any{"server": "pi"}
		for k, v := range tc.args {
			args[k] = v
		}
		var sent []string
		answeringRemote(t, tc.out, &sent)
		result, err := remoteServer(t).executeTool(tc.tool, args)
		if err != nil {
			t.Errorf("%s from %q: %v", tc.tool, tc.out, err)
			continue
		}
		data, _ := json.Marshal(util.EmptyLists(result))
		if string(data) != tc.want {
			t.Errorf("%s from %q = %s, want %s", tc.tool, tc.out, data, tc.want)
		}
	}
}

// An answer in neither shape is an error, not an empty result that reads as
// "nothing is listening" or "nothing happened". JSON in the wrong shape says
// so; only output that is not JSON is called invalid JSON.
func TestRemoteAnswersInNeitherShapeAreRefused(t *testing.T) {
	const shape, invalid = "unexpected answer from remote", "invalid JSON from remote"
	cases := []struct {
		tool string
		args map[string]any
		out  string
		want string
	}{
		{"open_ports", nil, `{"ports":{"a":1}}`, shape},
		{"open_ports", nil, `{"ports":"22"}`, shape},
		{"open_ports", nil, `"22"`, shape},
		{"open_ports", nil, `{"hostname":"pi"}`, shape},
		{"open_ports", nil, `{"ports":[`, invalid},
		{"processes", nil, `{"processes":{"a":1},"total":1}`, shape},
		{"processes", nil, `{"hostname":"pi"}`, shape},
		{"processes", nil, `null`, shape},
		{"processes", nil, `[{"pid":1,"name":"init"}]`, "older than 0.12.0"},
		{"watch_check", nil, `[]`, shape},
		{"watch_check", nil, "Checked 1 docker target(s).\n", invalid},
		{"watch_add", map[string]any{"container": "web"}, "error: something else\n", shape},
		{"watch_add", map[string]any{"container": "web"}, `[]`, shape},
		{"watch_remove", map[string]any{"container": "web"}, "nope\n", shape},
		{"watch_remove", map[string]any{"container": "web"}, `null`, shape},
	}
	for _, tc := range cases {
		args := map[string]any{"server": "pi"}
		for k, v := range tc.args {
			args[k] = v
		}
		var sent []string
		answeringRemote(t, tc.out, &sent)
		result, err := remoteServer(t).executeTool(tc.tool, args)
		if err == nil {
			data, _ := json.Marshal(result)
			t.Errorf("%s from %q answered %s, want an error", tc.tool, tc.out, data)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s from %q: error %q, want it to say %q", tc.tool, tc.out, err, tc.want)
		}
	}
}
