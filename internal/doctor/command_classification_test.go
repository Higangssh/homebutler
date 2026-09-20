package doctor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
)

// doctorCommands reads the commands doctor can print straight out of the
// source, rather than from a list kept beside it. A list would drift the first
// time someone adds a finding, which is the whole failure this guards.
//
// Two passes, because a command reaches r.add in more than one shape. The last
// argument of an r.add call catches commands that are not homebutler's, such as
// chmod. Every literal beginning "homebutler " catches the rest wherever it is
// written — including the ones built into a variable first, which the call-site
// pass alone would walk straight past.
func doctorCommands(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "doctor.go", nil, 0)
	if err != nil {
		t.Fatalf("parse doctor.go: %v", err)
	}

	seen := map[string]bool{}
	add := func(command string) {
		command = strings.TrimSpace(command)
		if command != "" {
			seen[command] = true
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			sel, ok := e.Fun.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "add" && len(e.Args) == 6 {
				add(leadingLiteral(e.Args[5]))
			}
		case *ast.BasicLit:
			if e.Kind == token.STRING {
				if value, err := strconv.Unquote(e.Value); err == nil && strings.HasPrefix(value, "homebutler ") {
					add(value)
				}
			}
		}
		return true
	})

	if len(seen) == 0 {
		t.Fatal("found no commands in doctor.go; the walk is looking in the wrong place")
	}
	commands := make([]string, 0, len(seen))
	for command := range seen {
		commands = append(commands, command)
	}
	return commands
}

// leadingLiteral digs out the first string constant of an expression, which is
// the fixed part of the command whatever is appended to it.
func leadingLiteral(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return ""
		}
		value, err := strconv.Unquote(e.Value)
		if err != nil {
			return ""
		}
		return value
	case *ast.BinaryExpr:
		return leadingLiteral(e.X)
	case *ast.CallExpr:
		// fmt.Sprintf("homebutler proxmox status --endpoint %q", name)
		if len(e.Args) > 0 {
			return leadingLiteral(e.Args[0])
		}
	}
	return ""
}

// Every command doctor prints has to be classified. #157 is about an agent
// being handed a command with no way to run it and no way to know that; a new
// finding that quietly adds another one should fail here rather than reach a
// release.
func TestEveryDoctorCommandIsClassified(t *testing.T) {
	for _, command := range doctorCommands(t) {
		runner, tool := classifyCommand(command)
		switch runner {
		case RunnerMCP:
			if tool == "" {
				t.Errorf("%q says a tool runs it and does not name one", command)
			}
		case RunnerCLI, RunnerShell:
			if tool != "" {
				t.Errorf("%q is %s and names the tool %q", command, runner, tool)
			}
		default:
			t.Errorf("%q is in neither commandTools nor cliOnly: decide which, and say why in cliOnly if no tool should run it", command)
		}
	}
}

// A tool named here that the registry does not have would be a command doctor
// tells an agent to run through something that does not exist.
func TestClassifiedToolsExist(t *testing.T) {
	for command, tool := range commandTools {
		if _, ok := capability.For(tool); !ok {
			t.Errorf("%q maps to %q, which is not in the capability registry", command, tool)
		}
	}
}

// The other direction: an entry nothing reaches is a decision about a command
// that no longer exists, and reads as though it still applies.
func TestNoDeadClassificationEntries(t *testing.T) {
	commands := doctorCommands(t)
	used := func(prefix string) bool {
		for _, command := range commands {
			if commandMatches(command, prefix) {
				return true
			}
		}
		return false
	}

	for prefix := range commandTools {
		if !used(prefix) {
			t.Errorf("commandTools has %q and doctor never prints it", prefix)
		}
	}
	for prefix := range cliOnly {
		if !used(prefix) {
			t.Errorf("cliOnly has %q and doctor never prints it", prefix)
		}
	}
}

// Every reason in cliOnly has to say something, since the entry is the record
// of a decision rather than a list of names.
func TestCLIOnlyEntriesGiveAReason(t *testing.T) {
	for command, reason := range cliOnly {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is cli-only and does not say why", command)
		}
	}
}

// "homebutler backup list" must not be answered by the entry for
// "homebutler backup", and a longer command must not be answered by a prefix
// that happens to match its first word.
func TestClassificationPrefersTheLongerMatch(t *testing.T) {
	if _, tool := classifyCommand("homebutler backup list"); tool != "backup_list" {
		t.Errorf("backup list resolved to %q", tool)
	}
	if _, tool := classifyCommand("homebutler backup"); tool != "backup_create" {
		t.Errorf("backup resolved to %q", tool)
	}
	if _, tool := classifyCommand("homebutler docker logs nginx"); tool != "docker_logs" {
		t.Errorf("docker logs with an argument resolved to %q", tool)
	}
	if runner, _ := classifyCommand("homebutler reporting-tool"); runner != "" {
		t.Errorf("a command that merely starts like report resolved to %q", runner)
	}
	if runner, _ := classifyCommand("chmod 600 /home/x/.config/homebutler/config.yaml"); runner != RunnerShell {
		t.Errorf("chmod resolved to %q", runner)
	}
}

// `homebutler backup drill --all` fell back to the entry for `homebutler
// backup` and named `backup_create`, so a finding telling the operator to
// drill would have sent an agent to take another backup instead. The test
// above could not see it: it checks that every printed command is classified,
// not that it is classified as the right thing, and those are different
// properties.
//
// The general version needs the cobra tree to know that `backup drill` is a
// command rather than `backup` with an argument, and that tree is not in this
// package. So the forms that reach a caller are pinned here, and
// docs/compatibility.md records that "classified" is what the walk above
// guarantees.
func TestDrillCommandsNameTheDrillTool(t *testing.T) {
	for _, command := range []string{
		"homebutler backup drill",
		"homebutler backup drill --all",
		"homebutler backup drill uptime-kuma",
	} {
		runner, tool := classifyCommand(command)
		if runner != RunnerMCP || tool != "backup_drill" {
			t.Errorf("%q → runner=%q tool=%q, want mcp/backup_drill", command, runner, tool)
		}
	}
}
