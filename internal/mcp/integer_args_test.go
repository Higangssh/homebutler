package mcp

import (
	"encoding/json"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/watch"
)

// These arguments were declared as strings until 0.40.0 and are integers now,
// because 1.0 freezes the schema and a count that is a string in the schema
// would stay one. Agents written against the old schema send "50", and the
// schema changing is no reason for their calls to start failing. So each one
// is read the way its handler reads it, from real JSON in both spellings, and
// the two answers have to agree.
var integerArgs = []struct {
	tool, arg string
	read      func(map[string]any) any
	sent      string
	want      any
}{
	// docker_logs forwards the count to the docker CLI, so it is read as the
	// string the CLI takes.
	{"docker_logs", "lines", func(a map[string]any) any { return stringArg(a, "lines") }, "50", "50"},
	{"install_app", "port", func(a map[string]any) any { return stringArg(a, "port") }, "8080", "8080"},
	{"processes", "limit", func(a map[string]any) any { return intArg(a, "limit", 10) }, "5", 5},
	{"watch_history", "limit", func(a map[string]any) any { return intArg(a, "limit", 10) }, "3", 3},
	{"report", "keep", func(a map[string]any) any { return intArg(a, "keep", 30) }, "7", 7},
	{"doctor", "backup_max_age_hours", func(a map[string]any) any { return intArg(a, "backup_max_age_hours", 168) }, "24", 24},
}

func TestAnIntegerArgumentStillAcceptsTheStringItUsedToBe(t *testing.T) {
	for _, c := range integerArgs {
		for _, raw := range []string{c.sent, `"` + c.sent + `"`} {
			var args map[string]any
			if err := json.Unmarshal([]byte(`{"`+c.arg+`":`+raw+`}`), &args); err != nil {
				t.Fatal(err)
			}
			if got := c.read(args); got != c.want {
				t.Errorf("%s %s=%s read as %#v, want %#v", c.tool, c.arg, raw, got, c.want)
			}
		}
	}
}

// Every integer in the registry is either in the table above or is vmid,
// which was an integer from the start and never took a string. A new integer
// argument then has to decide which it is rather than inherit whatever its
// handler happens to do.
func TestEveryIntegerArgumentIsInTheTable(t *testing.T) {
	listed := map[string]bool{}
	for _, c := range integerArgs {
		listed[c.tool+"."+c.arg] = true
	}
	for _, c := range capability.Registry {
		for name, p := range c.Tool.InputSchema.Properties {
			if p.Type != "integer" || name == "vmid" {
				continue
			}
			if !listed[c.Tool.Name+"."+name] {
				t.Errorf("%s.%s is an integer and is not in integerArgs", c.Tool.Name, name)
			}
		}
	}
}

// The table reads each argument through the helper its handler uses. These
// two go through the real entry points as well: lines through the gate every
// forwarded container argument passes, and limit through a whole call.
func TestIntegerArgumentsThroughTheirEntryPoints(t *testing.T) {
	for _, lines := range []any{float64(50), "50"} {
		if err := validateContainerArgs("docker_logs", map[string]any{"name": "web", "lines": lines}); err != nil {
			t.Errorf("docker_logs lines=%#v: %v", lines, err)
		}
	}

	s := NewServer(&config.Config{}, "dev", true)
	for _, limit := range []any{float64(1), "1"} {
		got, err := s.executeDemoTool("watch_history", map[string]any{"limit": limit})
		if err != nil {
			t.Fatalf("watch_history limit=%#v: %v", limit, err)
		}
		if n := len(got.([]watch.Incident)); n != 1 {
			t.Errorf("watch_history limit=%#v returned %d incidents, want 1", limit, n)
		}
	}
}
