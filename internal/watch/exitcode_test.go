package watch

import (
	"encoding/json"
	"testing"
	"time"
)

// A die event carries the exit status; dropping it meant every incident
// reached Analyze's `case 0` and was reported as a clean exit (#108).
func TestDockerEventReportsItsExitCode(t *testing.T) {
	// Captured from a real `docker events --format '{{json .}}'` die event.
	const raw = `{"status":"die","id":"abc","Actor":{"Attributes":{"execDuration":"2","exitCode":"137","image":"alpine","name":"hb-evt"}},"time":1788056105}`

	var ev dockerEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	code, ok := ev.exitCode()
	if !ok {
		t.Fatal("a die event's exit code was not read")
	}
	if code != 137 {
		t.Errorf("exit code = %d, want 137", code)
	}
}

// Absence and zero are different answers. Reporting a missing exit code as
// zero is the bug: the analyser reads zero as a clean exit.
func TestDockerEventDistinguishesMissingFromZero(t *testing.T) {
	cases := map[string]struct {
		raw       string
		wantCode  int
		wantFound bool
	}{
		"reported zero": {`{"Actor":{"Attributes":{"exitCode":"0"}}}`, 0, true},
		"absent":        {`{"Actor":{"Attributes":{"image":"alpine"}}}`, 0, false},
		"no attributes": {`{"Actor":{}}`, 0, false},
		"not a number":  {`{"Actor":{"Attributes":{"exitCode":"oops"}}}`, 0, false},
	}
	for name, tc := range cases {
		var ev dockerEvent
		if err := json.Unmarshal([]byte(tc.raw), &ev); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		code, ok := ev.exitCode()
		if ok != tc.wantFound || code != tc.wantCode {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", name, code, ok, tc.wantCode, tc.wantFound)
		}
	}
}

// The categories the exit code unlocks are the analyser's highest-confidence
// rules, and none of them could fire while the code never arrived.
func TestAnalyzeUsesTheExitCodeItIsGiven(t *testing.T) {
	cases := []struct {
		code     int
		category string
	}{
		{137, "oom"},
		{139, "segfault"},
		{143, "sigterm"},
		{0, "clean_restart"},
	}
	for _, tc := range cases {
		got := Analyze(CrashInfo{ExitCode: tc.code, Backend: "docker"})
		if got.Category != tc.category {
			t.Errorf("exit %d classified as %q, want %q", tc.code, got.Category, tc.category)
		}
	}
}

// An incident round-trips the exit code, so `watch show` can say how a process
// ended rather than only what its logs said.
func TestIncidentCarriesTheExitCodeThroughDisk(t *testing.T) {
	dir := t.TempDir()
	code := 137
	inc := Incident{
		ID:         GenerateIncidentID("svc", time.Now()),
		Container:  "svc",
		DetectedAt: time.Now(),
		ExitCode:   &code,
	}
	if err := SaveIncident(dir, &inc, 0); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}
	loaded, err := LoadIncident(dir, inc.ID)
	if err != nil {
		t.Fatalf("LoadIncident: %v", err)
	}
	if loaded.ExitCode == nil {
		t.Fatal("the exit code did not survive being written and read back")
	}
	if *loaded.ExitCode != 137 {
		t.Errorf("exit code = %d, want 137", *loaded.ExitCode)
	}
}

// An incident with no exit code must not serialize one, or a reader cannot
// tell "exited cleanly" from "nobody said".
func TestIncidentOmitsAnAbsentExitCode(t *testing.T) {
	data, err := json.Marshal(Incident{ID: "x", Container: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || contains(string(data), `"exit_code"`) {
		t.Errorf("an unreported exit code was serialized anyway: %s", data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
