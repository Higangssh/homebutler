package mcp

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
)

// localOnlyArgs are the arguments a server-capable tool takes and
// deliberately does not send to a server, each with the reason. It is empty:
// every argument is forwarded or refused. An entry here is a decision someone
// wrote down, not a gap the test below stopped noticing.
var localOnlyArgs = map[string]map[string]string{}

// remoteArgsBaseline is what a tool needs before a server will take the call
// at all, beyond its required arguments.
var remoteArgsBaseline = map[string]map[string]any{
	// A server answers format=json only; the default, mermaid, is refused.
	"inventory_export": {"format": "json"},
	// app is required unless all is set, which the schema cannot say.
	"backup_drill": {"app": "web"},
}

// probeValues are the values tried for one property: each is one the
// property's schema allows, and at least one is not its default.
func probeValues(name string, p capability.Property) []any {
	switch p.Type {
	case "boolean":
		return []any{true}
	case "integer":
		return []any{float64(7)}
	case "array":
		return []any{[]any{"/probe/" + name}}
	}
	if len(p.Enum) > 0 {
		values := make([]any, 0, len(p.Enum))
		for _, v := range p.Enum {
			values = append(values, v)
		}
		return values
	}
	return []any{"probe-" + name}
}

// sendFor is the argv a tool sends for args, or nil when it refused the call
// before reaching the remote.
func sendFor(t *testing.T, tool string, args map[string]any) []string {
	t.Helper()
	var sent []string
	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	runRemote = func(_ *config.ServerConfig, argv ...string) ([]byte, error) {
		sent = argv
		return []byte(`{}`), nil
	}
	call := map[string]any{"server": "pi"}
	for k, v := range args {
		call[k] = v
	}
	// The answer is not the question here; only what was sent is.
	_, _ = remoteServer(t).executeTool(tool, call)
	return sent
}

// A tool argument that never reaches the server is answered as if it had
// been: watch_add with a unit added the target under its display name, and
// backup_create with exclude archived the paths it was told to leave out.
// Both looked like success. This walks every argument of every tool that
// takes a server and fails when setting it changes nothing that is sent,
// unless the call is refused instead or the argument is in localOnlyArgs.
func TestEveryRemoteArgumentReachesTheServer(t *testing.T) {
	for tool, args := range localOnlyArgs {
		c, ok := capability.For(tool)
		if !ok || !c.Supports(capability.TargetServer) {
			t.Errorf("localOnlyArgs names %s, which cannot be pointed at a server", tool)
			continue
		}
		for arg := range args {
			if _, ok := c.Tool.InputSchema.Properties[arg]; !ok {
				t.Errorf("localOnlyArgs names %s.%s, which the tool does not take", tool, arg)
			}
		}
	}

	for _, def := range capability.Definitions() {
		c, _ := capability.For(def.Name)
		if !c.Supports(capability.TargetServer) {
			continue
		}
		schema := c.Tool.InputSchema
		base := map[string]any{}
		for k, v := range remoteArgsBaseline[def.Name] {
			base[k] = v
		}
		for _, req := range schema.Required {
			base[req] = probeValues("base-"+req, schema.Properties[req])[0]
		}
		baseSent := sendFor(t, def.Name, base)
		if baseSent == nil {
			t.Errorf("%s with %v was refused before reaching the server; give it a baseline in remoteArgsBaseline", def.Name, base)
			continue
		}

		for name, prop := range schema.Properties {
			if name == "server" {
				continue
			}
			if _, ok := localOnlyArgs[def.Name][name]; ok {
				continue
			}
			reached := false
			for _, v := range probeValues(name, prop) {
				if fmt.Sprint(base[name]) == fmt.Sprint(v) {
					continue
				}
				args := map[string]any{}
				for k, bv := range base {
					args[k] = bv
				}
				args[name] = v
				sent := sendFor(t, def.Name, args)
				if sent == nil {
					// Refused rather than run without it, which is not a drop.
					reached = true
					break
				}
				if slices.Equal(sent, baseSent) {
					continue
				}
				if want := probeText(v); want != "" && !strings.Contains(strings.Join(sent, " "), want) {
					t.Errorf("%s.%s=%v changed what was sent, but the value is not in it: %q", def.Name, name, v, sent)
				}
				reached = true
				break
			}
			if !reached {
				t.Errorf("%s.%s is dropped on the way to a server: %v is sent whatever it is set to; forward it, refuse it, or say why in localOnlyArgs", def.Name, name, baseSent)
			}
		}
	}
}

// probeText is the part of a probe value that has to appear in the argv.
func probeText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(v)
	case []any:
		if len(v) > 0 {
			return fmt.Sprint(v[0])
		}
	}
	return ""
}

// The remote reads each --exclude as a CSV record, so a path with a comma or
// a quote in it has to arrive as the one path it is.
func TestExcludedPathsArriveWhole(t *testing.T) {
	paths := []any{"/srv/media", "/srv/a,b", `/srv/say "hi"`, " /srv/leading-space"}
	sent := sendFor(t, "backup_create", map[string]any{"exclude": paths})

	fs := pflag.NewFlagSet("backup", pflag.ContinueOnError)
	got := fs.StringSlice("exclude", nil, "")
	fs.Bool("json", false, "")
	if err := fs.Parse(sent[1:]); err != nil {
		t.Fatalf("the remote could not parse %q: %v", sent, err)
	}
	want := []string{"/srv/media", "/srv/a,b", `/srv/say "hi"`, " /srv/leading-space"}
	if !slices.Equal(*got, want) {
		t.Fatalf("the remote reads %q, want %q (sent %q)", *got, want, sent)
	}
}

// --unit is newer than the remote may be. It is sent only when set, so a call
// without it still works against every release that has watch add.
func TestWatchAddLeavesUnitOffWhenUnset(t *testing.T) {
	sent := sendFor(t, "watch_add", map[string]any{"container": "web"})
	if slices.Contains(sent, "--unit") {
		t.Fatalf("sent --unit without one: %q", sent)
	}
	sent = sendFor(t, "watch_add", map[string]any{"container": "web", "kind": "systemd", "unit": "nginx.service"})
	if i := slices.Index(sent, "--unit"); i < 0 || i+1 >= len(sent) || sent[i+1] != "nginx.service" {
		t.Fatalf("unit not sent as --unit nginx.service: %q", sent)
	}
}
