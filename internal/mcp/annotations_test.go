package mcp

import (
	"encoding/json"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
)

type listedTool struct {
	Description string
	Annotations map[string]any
}

// listedTools decodes tools/list loosely, so a key that is present and a key
// that is absent stay different things.
func listedToolsFor(t *testing.T, risk capability.Risk) map[string]listedTool {
	t.Helper()
	s, out := newTestServer()
	if err := s.SetMaxRisk(risk); err != nil {
		t.Fatal(err)
	}
	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	raw, _ := json.Marshal(resp.Result)
	var list struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Annotations map[string]any `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]listedTool{}
	for _, tool := range list.Tools {
		got[tool.Name] = listedTool{tool.Description, tool.Annotations}
	}
	return got
}

// What a tool's hints have to be, written as the spec reads rather than
// copied from the code it checks. Both keys go out on every tool: a client
// that reads destructiveHint without checking readOnlyHint would otherwise
// see the spec's default, true, on every read.
func wantHints(c capability.Capability) map[string]any {
	switch c.Risk {
	case capability.RiskRead:
		return map[string]any{"readOnlyHint": true, "destructiveHint": false}
	case capability.RiskWrite:
		return map[string]any{"readOnlyHint": false, "destructiveHint": c.Erases == capability.ErasesSomething}
	default:
		return map[string]any{"readOnlyHint": false, "destructiveHint": true}
	}
}

func sameHints(got, want map[string]any) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func TestEveryToolsHintsMatchItsRiskClass(t *testing.T) {
	listed := listedToolsFor(t, capability.RiskDestructive)
	for _, c := range capability.Registry {
		got, ok := listed[c.Tool.Name]
		if !ok {
			t.Errorf("%s is not in tools/list", c.Tool.Name)
			continue
		}
		if want := wantHints(c); !sameHints(got.Annotations, want) {
			t.Errorf("%s is classed %s (erases %q) and tools/list annotates it %v, want %v", c.Tool.Name, c.Risk, c.Erases, got.Annotations, want)
		}
	}
}

// Under --max-risk read, report is offered only because no_save is forced on
// it, so in that mode it writes nothing, and both its hints and its
// description have to say so. Every other tool keeps its own.
func TestReadModeListsReportAsTheReadItIsThere(t *testing.T) {
	listed := listedToolsFor(t, capability.RiskRead)
	read := map[string]any{"readOnlyHint": true, "destructiveHint": false}
	for name, got := range listed {
		c, _ := capability.For(name)
		want := wantHints(c)
		if _, forced := readModeTools[name]; forced {
			want = read
		}
		if !sameHints(got.Annotations, want) {
			t.Errorf("under --max-risk read %s is annotated %v, want %v", name, got.Annotations, want)
		}
	}
	report, ok := listed["report"]
	if !ok {
		t.Fatal("read mode must still offer report")
	}
	if !sameHints(report.Annotations, read) {
		t.Errorf("report under --max-risk read is annotated %v; it runs as no_save there and writes nothing", report.Annotations)
	}
	c, _ := capability.For("report")
	if report.Description == c.Tool.Description {
		t.Errorf("report under --max-risk read keeps the description that says it saves a snapshot: %q", report.Description)
	}
}

// A tool offered in read mode with arguments forced on it needs a description
// for that mode, or the one it lists describes arguments it no longer obeys.
func TestEveryReadModeToolHasAReadModeDescription(t *testing.T) {
	for name := range readModeTools {
		if readModeDescriptions[name] == "" {
			t.Errorf("%s is offered in read mode with forced arguments and has no read-mode description", name)
		}
	}
}
