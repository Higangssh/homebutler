package remote

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuotePathExpandsHomeAndNothingElse(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/usr/local/bin/homebutler", "/usr/local/bin/homebutler"},
		{"/home/me/my bin/homebutler", "'/home/me/my bin/homebutler'"},
		{"~/.local/bin/homebutler", `"$HOME"/.local/bin/homebutler`},
		{"$HOME/.local/bin/homebutler", `"$HOME"/.local/bin/homebutler`},
		{"${HOME}/bin/home butler", `"$HOME"/'bin/home butler'`},
		{"~", `"$HOME"`},
		{"homebutler", "homebutler"},
	} {
		got, err := quotePath(c.in)
		if err != nil || got != c.want {
			t.Errorf("quotePath(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, refused := range []string{"$USER/bin/homebutler", "/opt/$(id)/homebutler", "~/bin/$X"} {
		if got, err := quotePath(refused); err == nil {
			t.Errorf("quotePath(%q) = %q; a variable other than a leading ~ or $HOME has to be refused", refused, got)
		}
	}
}

// The guard against #303 coming back: in this package, util.ShellQuote is only
// called by quotePath and quoteLiteral. A remote path quoted anywhere else is
// a path whose ~ or $HOME the remote shell will never see.
func TestShellQuoteGoesThroughThePathOrLiteralHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name == "quotePath" || fn.Name.Name == "quoteLiteral" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ShellQuote" {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "util" {
						t.Errorf("%s: %s calls util.ShellQuote directly; use quotePath for a path or quoteLiteral for text that must stay as written", fset.Position(call.Pos()), fn.Name.Name)
					}
				}
				return true
			})
		}
	}
}

// What scp prints on a failure is the only thing that says why, and it used
// to be dropped. Recorded from a real `scp -t` on the Pi.
func TestReasonKeepsWhatScpSaid(t *testing.T) {
	got := reason([]byte("\x01scp: $HOME/.local/bin/hb-probe: No such file or directory\nscp: protocol error: lost connection\n"))
	if !strings.Contains(got, "No such file or directory") {
		t.Errorf("reason dropped scp's message: %q", got)
	}
	if strings.ContainsAny(got, "\x00\x01\x02") {
		t.Errorf("reason kept scp's control bytes: %q", got)
	}
	if reason(nil) != "" || reason([]byte("\x00")) != "" {
		t.Error("nothing printed should add nothing to the error")
	}
}
