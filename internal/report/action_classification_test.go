package report

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/Higangssh/homebutler/internal/doctor"
)

// Report's suggested actions go through the same classifier doctor's findings
// do, and doctor has a test that every command it prints is classified. This
// side had none, so an action written as `action("Fix it: homebutler newcmd")`
// would reach a caller with a command it is told to check `runner` before
// running, and no `runner` to check — `runner` is `omitempty`, so the field
// would simply not be there.
func TestEverySuggestedActionIsClassified(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "report.go", nil, 0)
	if err != nil {
		t.Fatalf("parse report.go: %v", err)
	}

	var texts []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "action" || len(call.Args) != 1 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if text, err := strconv.Unquote(lit.Value); err == nil {
			texts = append(texts, text)
		}
		return true
	})

	if len(texts) == 0 {
		t.Fatal("found no action() literals in report.go: this test has stopped watching anything")
	}

	for _, text := range texts {
		a := action(text)
		if a.Command == "" {
			// Nothing to run, so there is nothing to classify and no runner
			// to promise. That is the only case where the field is absent.
			if a.Runner != "" {
				t.Errorf("%q has no command but says %q runs it", text, a.Runner)
			}
			continue
		}
		switch a.Runner {
		case doctor.RunnerMCP:
			if a.Tool == "" {
				t.Errorf("%q says a tool runs it and does not name one", a.Command)
			}
		case doctor.RunnerCLI, doctor.RunnerShell:
			if a.Tool != "" {
				t.Errorf("%q is %s and names the tool %q", a.Command, a.Runner, a.Tool)
			}
		default:
			t.Errorf("%q carries a command with no runner: an agent is told to check runner before offering a fix, and there is nothing to check. Add it to commandTools or cliOnly in internal/doctor", a.Command)
		}
	}

	// What the walk above cannot see: the shape a future action would take.
	// An unknown homebutler command is the case that reaches a caller with a
	// command and no runner, so it is pinned here rather than left to whoever
	// writes the next action.
	if a := action("Do the thing: homebutler not-a-real-command"); a.Command == "" || a.Runner != "" {
		t.Errorf("an unknown homebutler command classified as command=%q runner=%q", a.Command, a.Runner)
	}
}
