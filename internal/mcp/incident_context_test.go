package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/watch"
)

// seedIncidentHome writes, under a temporary HOME, one incident for web at
// 03:12 and the two snapshots either side of it, the second with web's image
// moved and redis gone.
func seedIncidentHome(t *testing.T, withAfter bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	watchDir := filepath.Join(home, ".homebutler", "watch")
	inc := watch.Incident{ID: "web-20261124-031200.000-abcdef", Container: "web", DetectedAt: time.Date(2026, 11, 24, 3, 12, 0, 0, time.UTC), PreLogs: "panic: boom"}
	if err := watch.SaveIncident(watchDir, &inc, 0); err != nil {
		t.Fatal(err)
	}

	snapDir := filepath.Join(home, ".homebutler", "reports", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	snaps := map[string]string{
		"snapshot_20261124T030000Z.json": `{"timestamp":"2026-11-24T03:00:00Z","server_name":"pi","containers":[{"id":"a","name":"web","image":"web:1","state":"running"},{"id":"b","name":"redis","image":"redis:7","state":"running"}],"ports":[],"processes":[{"name":"sshd"}]}`,
	}
	if withAfter {
		snaps["snapshot_20261124T040000Z.json"] = `{"timestamp":"2026-11-24T04:00:00Z","server_name":"pi","containers":[{"id":"a","name":"web","image":"web:2","state":"running"}],"ports":[],"processes":[{"name":"sshd"}]}`
	}
	for name, body := range snaps {
		if err := os.WriteFile(filepath.Join(snapDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The tool is a read, so a read-only server offers it and answers it, and the
// answer is the incident with the changes split by whose they are.
func TestIncidentContextAnswersUnderReadOnly(t *testing.T) {
	seedIncidentHome(t, true)
	s, out := newTestServer()
	if err := s.SetMaxRisk(capability.RiskRead); err != nil {
		t.Fatal(err)
	}

	list := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, _ := json.Marshal(list.Result)
	if !strings.Contains(string(raw), `"incident_context"`) {
		t.Fatal("a read-only server does not offer incident_context")
	}

	resp := sendAndReceive(t, s, out, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"incident_context","arguments":{"container":"web"}}}`)
	raw, _ = json.Marshal(resp.Result)
	var result toolsCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("incident_context failed: %s", raw)
	}
	var got report.IncidentContext
	if err := json.Unmarshal([]byte(result.Content[0].Text), &got); err != nil {
		t.Fatalf("answer is not an IncidentContext: %v\n%s", err, result.Content[0].Text)
	}
	if got.Incident.ID != "web-20261124-031200.000-abcdef" || !got.Compared || got.AfterIsCurrent {
		t.Fatalf("answer = %+v", got)
	}
	if got.Incident.PreLogs != "" {
		t.Error("logs came back without include_logs")
	}
	if len(got.AboutTarget) != 1 || got.AboutTarget[0].Kind != "image" || got.AboutTarget[0].Target != "web" {
		t.Errorf("about_target = %+v, want web's image change", got.AboutTarget)
	}
	if len(got.OtherChanges) != 1 || got.OtherChanges[0].Target != "redis" {
		t.Errorf("other_changes = %+v, want redis gone", got.OtherChanges)
	}
}

// Inside a container the current state is the container's, so an incident
// with no snapshot after it is answered uncompared rather than measured
// against the wrong machine.
func TestIncidentContextInAContainerDoesNotReadTheContainerAsTheHost(t *testing.T) {
	seedIncidentHome(t, false)
	t.Setenv("HOMEBUTLER_CONTAINER", "1")
	s, _ := newTestServer()

	answer, err := s.executeTool("incident_context", map[string]any{"id": "web-20261124-031200.000-abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	got := answer.(*report.IncidentContext)
	if got.Compared || got.Before != "2026-11-24T03:00:00Z" || !strings.Contains(got.Note, "cannot be read from here") {
		t.Errorf("in a container: compared = %v, before = %q, note = %q", got.Compared, got.Before, got.Note)
	}
}

// The ID goes after --, so an ID that looks like a flag reaches the server
// as an ID, and the server refuses it as one.
func TestIncidentContextSendsTheIDAfterTheFlags(t *testing.T) {
	s, _ := newTestServer()
	s.cfg.Servers = []config.ServerConfig{{Name: "pi", Host: "192.0.2.1"}}
	var sent []string
	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	runRemote = func(_ *config.ServerConfig, argv ...string) ([]byte, error) {
		sent = argv
		return []byte(`{"incident":{},"compared":false,"after_is_current":false,"about_target":[],"other_changes":[]}`), nil
	}

	if _, err := s.executeTool("incident_context", map[string]any{"server": "pi", "id": "--help", "include_logs": true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"watch", "show", "--context", "--json", "--logs", "--", "--help"}
	if !slices.Equal(sent, want) {
		t.Errorf("sent %q, want %q", sent, want)
	}
}
