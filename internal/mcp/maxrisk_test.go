package mcp

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/capability"
)

func listedNames(t *testing.T, risk capability.Risk) []string {
	t.Helper()
	s, out := newTestServer()
	if err := s.SetMaxRisk(risk); err != nil {
		t.Fatal(err)
	}
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	raw, _ := json.Marshal(resp.Result)
	var list toolsListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range list.Tools {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}

func expectedNames(keep func(capability.Capability) bool) []string {
	var names []string
	for _, c := range capability.Registry {
		if keep(c) {
			names = append(names, c.Tool.Name)
		}
	}
	sort.Strings(names)
	return names
}

// Each ceiling's tools/list follows the registry, so a tool added or
// reclassified moves between modes without this test being edited. The golden
// records the default; this records the other two.
func TestEachCeilingListsWhatTheRegistrySays(t *testing.T) {
	cases := map[capability.Risk]func(capability.Capability) bool{
		capability.RiskDestructive: func(capability.Capability) bool { return true },
		capability.RiskWrite:       func(c capability.Capability) bool { return c.Risk != capability.RiskDestructive },
		capability.RiskRead: func(c capability.Capability) bool {
			_, forced := readModeTools[c.Tool.Name]
			return c.Risk == capability.RiskRead || forced
		},
	}
	for risk, keep := range cases {
		got, want := listedNames(t, risk), expectedNames(keep)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("--max-risk %s lists\n %v\nwant\n %v", risk, got, want)
		}
	}
	if got := listedNames(t, capability.RiskRead); !contains(got, "report") {
		t.Error("read mode must still offer report")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Hiding a tool from the list is not a limit: a client that knows its name can
// call it. Every tool a ceiling leaves out is refused, and the refusal says
// which ceiling and which class.
func TestEveryToolAboveTheCeilingIsRefusedByName(t *testing.T) {
	for _, risk := range []capability.Risk{capability.RiskRead, capability.RiskWrite} {
		listed := listedNames(t, risk)
		for _, c := range capability.Registry {
			if contains(listed, c.Tool.Name) {
				continue
			}
			s, out := newTestServer()
			_ = s.SetMaxRisk(risk)
			// Demo mode answers every tool with canned data. With the gate
			// in place the call never gets there; if the gate ever breaks,
			// this test fails without stopping a container, purging an app
			// or sending a wake packet on the machine running it.
			s.demo = true
			r := callOn(t, s, out, c.Tool.Name, map[string]any{"name": "web", "app": "web", "archive": "x.tar.gz", "target": "nas"})
			text := ""
			if len(r.Content) > 0 {
				text = r.Content[0].Text
			}
			want := fmt.Sprintf("this server was started with --max-risk %s, and %s is classed %s", risk, c.Tool.Name, c.Risk)
			if !r.IsError || !strings.Contains(text, want) {
				t.Errorf("--max-risk %s: %s was not refused: %q", risk, c.Tool.Name, text)
			}
		}
	}
}

func callOn(t *testing.T, s *Server, out interface{ Reset() }, name string, args map[string]any) toolsCallResult {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	s.in = strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":%s}`, params) + "\n")
	buf := s.out.(interface{ Bytes() []byte })
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	var resp jsonRPCResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("parse %q: %v", buf.Bytes(), err)
	}
	raw, _ := json.Marshal(resp.Result)
	var r toolsCallResult
	_ = json.Unmarshal(raw, &r)
	return r
}

// tree records every file and directory under root with its size and mtime.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		m[p] = fmt.Sprintf("%v %d %s", d.IsDir(), info.Size(), info.ModTime().Format(time.RFC3339Nano))
		return nil
	})
	return m
}

// "Changes nothing" is checked against everything homebutler keeps on disk,
// not a file count: report is called with no_save false and keep 1, which
// would save a snapshot and prune the rest if the mode did not stop it.
// network_scan and the Proxmox reads are left out here only because a unit
// test cannot wait on a subnet sweep or an API; they hold no state either.
func TestReadModeLeavesEverythingOnDiskAsItWas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	snapshots := filepath.Join(home, ".homebutler", "reports", "snapshots")
	if err := os.MkdirAll(snapshots, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshots, "snapshot_20260101T000000Z.json"), []byte(`{"timestamp":"2026-01-01T00:00:00Z","processes":[{"name":"init"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshots, "snapshot_20260102T000000Z.json"), []byte(`{"timestamp":"2026-01-02T00:00:00Z","processes":[{"name":"init"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := tree(t, home)

	skip := map[string]bool{"network_scan": true}
	for _, name := range listedNames(t, capability.RiskRead) {
		if skip[name] || strings.HasPrefix(name, "proxmox_") && name != "proxmox_script_list" && name != "proxmox_script_command" {
			continue
		}
		s, out := newTestServer()
		_ = s.SetMaxRisk(capability.RiskRead)
		args := map[string]any{"no_save": false, "keep": 1, "app": "uptime-kuma", "slug": "docker", "name": "web"}
		callOn(t, s, out, name, args)
	}

	after := tree(t, home)
	for p, v := range after {
		if before[p] != v {
			t.Errorf("read mode changed %s: %q -> %q", strings.TrimPrefix(p, home), before[p], v)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("read mode removed %s", strings.TrimPrefix(p, home))
		}
	}
}

// The same report call without the ceiling does save, so the test above is
// not passing because report never writes under test.
func TestReportStillSavesWithoutTheCeiling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, out := newTestServer()
	callOn(t, s, out, "report", map[string]any{"no_save": false})
	entries, _ := os.ReadDir(filepath.Join(home, ".homebutler", "reports", "snapshots"))
	if len(entries) != 1 {
		t.Fatalf("default mode should save one snapshot, found %d", len(entries))
	}
}

// A reduced list depends on how this process was started, so a shared cache
// must not hand it to anyone else (MCP caching: "public" may be served to any
// caller). The full list stays public, as it was.
func TestOnlyTheFullListIsPublicToCaches(t *testing.T) {
	for risk, want := range map[capability.Risk]string{
		capability.RiskDestructive: "public",
		capability.RiskWrite:       "private",
		capability.RiskRead:        "private",
	} {
		s, out := newTestServer()
		_ = s.SetMaxRisk(risk)
		resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
		raw, _ := json.Marshal(resp.Result)
		var list toolsListResult
		_ = json.Unmarshal(raw, &list)
		if list.CacheScope != want {
			t.Errorf("--max-risk %s: cacheScope %q, want %q", risk, list.CacheScope, want)
		}
	}
}

// A tool whose class is empty or misspelt is treated as destructive: offered
// only when everything is. Read as a map lookup it would rank with read and
// be allowed in read mode.
func TestAnUnknownClassIsTreatedAsDestructive(t *testing.T) {
	for _, risk := range []capability.Risk{"", "reed", "Read"} {
		for ceiling, want := range map[capability.Risk]bool{
			capability.RiskRead:        false,
			capability.RiskWrite:       false,
			capability.RiskDestructive: true,
		} {
			s, _ := newTestServer()
			_ = s.SetMaxRisk(ceiling)
			if got, _ := s.allowedFor("made_up_tool", risk); got != want {
				t.Errorf("class %q under --max-risk %s: allowed=%v, want %v", risk, ceiling, got, want)
			}
		}
	}
}
