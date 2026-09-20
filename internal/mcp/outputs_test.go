package mcp

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"

	"github.com/Higangssh/homebutler/internal/capability"
)

// The registry froze how a tool is called and said nothing about what comes
// back — which is the half an agent branches on. This is the mechanism that
// stops the next tool being added without that decision being made: getting
// the classification wrong is still possible, leaving it out is not.
func TestEveryToolDeclaresWhatItAnswersWith(t *testing.T) {
	for _, c := range capability.Registry {
		out, ok := ToolOutputs[c.Tool.Name]
		if !ok {
			t.Errorf("%s is in the registry and does not say what it answers with: add it to ToolOutputs as frozen(...) or passthrough(...)", c.Tool.Name)
			continue
		}
		switch {
		case out.Frozen == nil && out.Passthrough == "":
			t.Errorf("%s declares neither a shape nor whose shape it is", c.Tool.Name)
		case out.Frozen != nil && out.Passthrough != "":
			t.Errorf("%s declares both a frozen shape and a passthrough; it is one or the other", c.Tool.Name)
		}
	}
}

// The other direction: a declaration for a tool that no longer exists is a
// line nobody will delete, and it would keep a type in the frozen surface that
// nothing returns.
func TestEveryDeclarationNamesARealTool(t *testing.T) {
	for name := range ToolOutputs {
		if _, ok := capability.For(name); !ok {
			t.Errorf("ToolOutputs names %q, which is not in the registry", name)
		}
	}
}

// demoArgs are the arguments a tool needs before it will answer at all.
// Tools not named here take none.
var demoArgs = map[string]map[string]any{
	"docker_restart":         {"name": "nginx"},
	"docker_stop":            {"name": "nginx"},
	"docker_logs":            {"name": "nginx"},
	"docker_top":             {"name": "nginx"},
	"docker_inspect":         {"name": "nginx"},
	"wake":                   {"target": "desk"},
	"watch_add":              {"container": "nginx"},
	"watch_remove":           {"container": "nginx"},
	"backup_restore":         {"archive": "demo.tar.gz"},
	"backup_drill":           {"app": "uptime-kuma"},
	"proxmox_script_command": {"slug": "docker"},
	"install_app":            {"app": "uptime-kuma"},
	"install_status":         {"app": "uptime-kuma"},
	"install_uninstall":      {"app": "uptime-kuma"},
	"install_purge":          {"app": "uptime-kuma"},
	"proxmox_node":           {"node": "pve"},
	"proxmox_task_status":    {"node": "pve", "upid": "UPID:pve:0"},
	"proxmox_guest_start":    {"endpoint": "pve", "node": "pve", "type": "qemu", "vmid": 100.0, "confirm": true},
	"proxmox_guest_reboot":   {"endpoint": "pve", "node": "pve", "type": "qemu", "vmid": 100.0, "confirm": true},
	"proxmox_guest_shutdown": {"endpoint": "pve", "node": "pve", "type": "qemu", "vmid": 100.0, "confirm": true},
}

// Demo mode is what docs/mcp-server.md offers an agent to try first, so it is
// the first shape a lot of callers ever see from homebutler. It was built from
// map literals that nothing compared against the types they were imitating,
// and it drifted: its report answered with "Containers: 5 running, 1 stopped"
// as a sentence, two releases after the typed counts were added to stop a
// caller having to parse exactly that.
//
// A missing field here is worse than a wrong one. A wrong value is a lie
// somebody eventually notices; a missing field teaches a caller the field does
// not exist.
func TestDemoAnswersWithTheShapeTheToolDeclares(t *testing.T) {
	s := NewServer(demoConfig(), "test", true)

	for _, c := range capability.Registry {
		name := c.Tool.Name
		out := ToolOutputs[name]
		if out.Frozen == nil {
			continue // not ours to hold a shape for
		}

		result, err := s.executeDemoTool(name, demoArgs[name])
		if err != nil {
			t.Errorf("%s: demo refused its own arguments: %v", name, err)
			continue
		}
		missing := missingFields(t, out.Frozen, result)
		if len(missing) > 0 {
			t.Errorf("%s: the demo answer is missing %v — a caller meeting demo first would learn those fields do not exist", name, missing)
		}
	}
}

func demoConfig() *config.Config { return &config.Config{} }

// missingFields names the json keys the declared shape has and the answer does
// not. It compares keys rather than Go types because a caller reads JSON, and
// it walks one level: a nested object that is present but hollow is a separate
// question from a field that is not there at all.
func missingFields(t *testing.T, want any, got any) []string {
	t.Helper()

	// A slice marshals to an array, not an object, so comparing the two
	// directly compared nothing: ten tools declare `frozen([]T{})` and every
	// one of them passed without a single field being looked at. Take the
	// element type's keys instead, and the first element of the answer.
	if reflect.TypeOf(want).Kind() == reflect.Slice {
		elem := reflect.New(reflect.TypeOf(want).Elem()).Elem().Interface()
		first, ok := firstElement(got)
		if !ok {
			// An empty list teaches a caller nothing about the shape, which
			// is the one thing a demo is for.
			return []string{"(the demo answered with an empty list)"}
		}
		want, got = elem, first
	}

	wantKeys := requiredKeys(reflect.TypeOf(want))
	gotKeys := jsonKeys(t, got)
	if wantKeys == nil {
		return nil // a scalar: nothing to be missing
	}

	var missing []string
	for _, k := range wantKeys {
		if !slices.Contains(gotKeys, k) {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

// jsonKeys marshals a value and returns its top-level object keys, or nil when
// it does not marshal to an object.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// The adapters marshal the demo maps into the declared types, which fixes the
// shape and would also swallow a key the type does not have — a silent version
// of the `section`/`field` defect. This is what stops that: every key in the
// demo data has to exist in the type it feeds.
func TestDemoDataHasNoKeysItsTypeLacks(t *testing.T) {
	for _, server := range []string{"", "nas-box", "raspberry-pi"} {
		checkKeys(t, "demoStatus", demoStatus(server), system.StatusInfo{})
		for _, p := range demoPorts(server) {
			checkKeys(t, "demoPorts", p, ports.PortInfo{})
		}
		if list, ok := demoDocker(server)["containers"].([]map[string]any); ok {
			for _, c := range list {
				checkKeys(t, "demoDocker", c, docker.Container{})
			}
		}
	}
}

func checkKeys(t *testing.T, what string, data any, target any) {
	t.Helper()

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		return
	}
	// From the type's tags, not from marshalling a zero value: an omitempty
	// field is absent from the zero value and would look unknown here.
	known := tagKeys(reflect.TypeOf(target))
	for k := range got {
		if !slices.Contains(known, k) {
			t.Errorf("%s carries %q, which %T does not have — it would be dropped on the way to a caller", what, k, target)
		}
	}
}

// tagKeys is every json key a type can emit, omitempty included.
func tagKeys(t reflect.Type) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}

// firstElement returns the first element of a list-shaped value.
func firstElement(v any) (any, bool) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	if rv.Len() == 0 {
		return nil, false
	}
	return rv.Index(0).Interface(), true
}

// requiredKeys is every json key a type always emits. An omitempty field is
// allowed to be absent — that is what the tag means — so demanding it here
// would make the check fail on answers that are correct.
func requiredKeys(t reflect.Type) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || strings.Contains(opts, "omitempty") {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}
