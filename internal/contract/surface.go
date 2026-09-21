// Package contract describes the part of homebutler that other people build
// on, in a form a test can compare byte for byte.
//
// CONTRIBUTING says 1.0 freezes the MCP tool surface and the JSON schema. That
// promise is the reason several changes landed before 1.0 rather than after,
// and until now the bytes it covers were spread across the capability
// registry, the HTTP handlers and whatever each command happened to marshal.
// A freeze nobody can check is not a freeze: the only way to notice a renamed
// field was to notice it.
//
// So the surface is written down here, rendered deterministically, and held by
// a golden file. Renaming a tool, retyping a field, changing a risk class or
// dropping the token requirement from a route all fail the build with a diff,
// which makes each of them a deliberate act with a changelog line rather than
// an accident somebody finds in four months.
package contract

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/mcp"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/server"
	"github.com/Higangssh/homebutler/internal/system"
)

// Surface renders everything 1.0 freezes. The output is sorted rather than
// emitted in declaration order, so moving a registry entry is not a diff and a
// rename is.
func Surface() string {
	var b strings.Builder

	b.WriteString("# The surface homebutler freezes at 1.0.\n")
	b.WriteString("# Regenerate with: go test ./internal/contract -update\n")
	b.WriteString("# What this covers, and what it deliberately does not, is in docs/compatibility.md.\n")

	b.WriteString("\n## tools\n")
	for _, line := range toolLines() {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n## routes\n")
	for _, line := range routeLines() {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n## outputs\n")
	for _, line := range outputLines() {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n## absent\n")
	for _, line := range absentLines() {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n## json\n")
	for _, line := range jsonLines() {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n## vocabularies\n")
	b.WriteString("report.kind: " + strings.Join(report.Kinds(), " ") + "\n")
	b.WriteString("doctor.runner: " + strings.Join([]string{doctor.RunnerMCP, doctor.RunnerCLI, doctor.RunnerShell}, " ") + "\n")
	b.WriteString("doctor.category: " + strings.Join(doctor.Categories(), " ") + "\n")

	return b.String()
}

// toolLines is one line per MCP tool: its name, what calling it may do, and the
// arguments it takes. Descriptions are prose and are left out — they are the
// one part of a tool that 1.0 does not freeze.
func toolLines() []string {
	lines := make([]string, 0, len(capability.Registry))
	for _, c := range capability.Registry {
		var args []string
		for name, property := range c.Tool.InputSchema.Properties {
			args = append(args, name+":"+property.Type)
		}
		sort.Strings(args)

		required := append([]string(nil), c.Tool.InputSchema.Required...)
		sort.Strings(required)

		line := fmt.Sprintf("%s risk=%s args=[%s]", c.Tool.Name, c.Risk, strings.Join(args, " "))
		if len(required) > 0 {
			line += " required=[" + strings.Join(required, " ") + "]"
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}

// routeLines covers what a dashboard, a widget or a script can reach over HTTP,
// and what it costs to reach it. The protection is the half worth freezing: a
// route quietly losing its token requirement is the change nobody would see.
//
// The routes are read from a server rather than from the registry, because
// half of them are not capabilities — GET /api/report is what the widget in
// docs/web-dashboard.md reads, and it is registered by hand. Two servers are
// built, one with a token and one without, and the difference between them is
// exactly the set of routes that a token buys.
func routeLines() []string {
	open := server.New(&config.Config{}, "127.0.0.1", 8080)

	guarded := server.New(&config.Config{}, "127.0.0.1", 8080)
	guarded.SetToken("contract")

	without := map[string]bool{}
	for _, route := range open.Routes() {
		without[route] = true
	}

	var lines []string
	for _, route := range guarded.Routes() {
		protection := "token"
		if without[route] {
			protection = "none"
		}
		lines = append(lines, fmt.Sprintf("%s protection=%s", route, protection))
	}
	sort.Strings(lines)
	return lines
}

// outputLines records what each tool answers with: the type when the shape is
// ours, or the word passthrough when it is not. The registry froze how a tool
// is called and said nothing about what comes back, which is the half an agent
// branches on — and a tool quietly moving from one to the other, or changing
// the type it answers with, is a change a caller feels.
//
// The reason string for a passthrough is not here. It is prose and improves;
// that a tool is one is the part worth freezing.
func outputLines() []string {
	var out []string
	for _, c := range capability.Registry {
		name := c.Tool.Name
		o, ok := mcp.ToolOutputs[name]
		switch {
		case !ok:
			out = append(out, name+": UNDECLARED")
		case o.Frozen != nil:
			out = append(out, name+": "+typeName(reflect.TypeOf(o.Frozen)))
		default:
			out = append(out, name+": passthrough")
		}
	}
	sort.Strings(out)
	return out
}

// absentLines records which capabilities the dashboard cannot reach and which
// decision each is waiting on. The reasons go out of GET /api/capabilities, so
// they are something we say rather than something we note — and the answer to
// "how much is this surface still going to grow" lives here. A capability
// moving from one reason to another, or acquiring a route, is then a diff.
func absentLines() []string {
	var out []string
	for _, c := range capability.Registry {
		if c.Exposed() {
			continue
		}
		out = append(out, c.Tool.Name+": "+absentKey(c.HTTP.Absent))
	}
	sort.Strings(out)
	return out
}

// absentKey names the constant rather than repeating its sentence. The
// sentences are prose and get better; which decision a capability waits on is
// the part that is frozen.
func absentKey(reason string) string {
	switch reason {
	case capability.AbsentNoViewYet:
		return "no-view-yet"
	case capability.AbsentNoActionRuleYet:
		return "no-action-rule-yet"
	case capability.AbsentNoDestructiveRuleYet:
		return "no-destructive-rule-yet"
	default:
		return "UNKNOWN(" + reason + ")"
	}
}

// jsonLines records the field names and types a caller receives. Types are
// included because a field that changes from a string to an object breaks a
// caller exactly as thoroughly as one that disappears.
func jsonLines() []string {
	types := []struct {
		name  string
		value any
	}{
		{"report.Report", report.Report{}},
		{"report.ChangeLine", report.ChangeLine{}},
		{"report.Finding", report.Finding{}},
		{"report.Action", report.Action{}},
		{"doctor.Result", doctor.Result{}},
		{"doctor.Finding", doctor.Finding{}},
		{"doctor.Summary", doctor.Summary{}},
		{"system.StatusInfo", system.StatusInfo{}},
	}

	var lines []string
	for _, t := range types {
		for _, field := range jsonFields(reflect.TypeOf(t.value), map[reflect.Type]bool{}) {
			lines = append(lines, t.name+"."+field)
		}
	}
	return lines
}

// jsonFields reads the json tags rather than the Go names, because the tag is
// what a caller sees. A field with no tag is reported under its Go name, which
// is also what encoding/json would emit.
//
// Nested structs are walked, not just named. A caller reads
// `status.cpu.usage_percent`, so recording only `cpu:CPUInfo` would let that
// field be renamed without the contract noticing — which is the whole thing
// this file exists to prevent.
func jsonFields(t reflect.Type, seen map[reflect.Type]bool) []string {
	if seen[t] {
		return nil // a type that contains itself; one level is enough to pin it
	}
	seen[t] = true
	defer delete(seen, t)

	var out []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported: not part of anything anybody receives
		}

		tag := field.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		switch name {
		case "-":
			continue
		case "":
			name = field.Name
		}

		// A key that may be absent and a key whose value may be null are two
		// different things to a caller, and dropping `omitempty` here meant a
		// field becoming optional was invisible to the golden. The two marks
		// sit in the two positions they describe: `name?` is the key that can
		// go missing, `?type` is the value that can be null.
		if hasOption(opts, "omitempty") {
			name += "?"
		}

		out = append(out, name+":"+typeName(field.Type))
		if nested := structUnder(field.Type); nested != nil {
			for _, child := range jsonFields(nested, seen) {
				out = append(out, name+"."+child)
			}
		}
	}
	sort.Strings(out)
	return out
}

func hasOption(opts, want string) bool {
	for opts != "" {
		var opt string
		opt, opts, _ = strings.Cut(opts, ",")
		if opt == want {
			return true
		}
	}
	return false
}

// structUnder finds the struct a field is made of, through pointers and
// slices, or nil when the field is not made of one. time.Time is left alone:
// it marshals as a string, and its internals are not anybody's contract.
func structUnder(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t.PkgPath() == "time" {
		return nil
	}
	return t
}

// A pointer field marshals as `null` when it is not set, and a caller that was
// reading a number has to handle that. So nullability is part of the type the
// golden freezes — `?int` is not `int` — and a field that quietly becomes
// nullable shows up as a change rather than as nothing at all.
func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return "?" + typeName(t.Elem())
	case reflect.Slice:
		return "[]" + typeName(t.Elem())
	case reflect.Map:
		return "map[" + typeName(t.Key()) + "]" + typeName(t.Elem())
	case reflect.Struct:
		return t.Name()
	default:
		return t.Kind().String()
	}
}
