package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A hint that names a command which does not exist sends the reader to an
// error. It has happened twice: init told people to run `homebutler tui`
// (#159), and install finished by suggesting `homebutler logs <app>`, which
// the 0.40.0 soak ran and got "unknown command". Neither was caught, because
// nothing read the strings a command prints.
//
// So every place a string tells someone to run homebutler is resolved
// against the command tree, the way the skill's commands are. "Tells someone
// to run" is read from what comes before the name — Run:, try, sudo, a
// backtick, a quote, a colon, or the indentation of an example — because
// "homebutler does not run a daemon" is a sentence and not a command.
func TestEveryCommandAHintSuggestsExists(t *testing.T) {
	mention := regexp.MustCompile("(?m)(Run: |run |Run |[Tt]ry:? |sudo |`|'|\\$ |: |^[ \\t]{2,})(homebutler [^`'\"\\n)]*)")

	var checked int
	for _, root := range []string{".", "../internal"} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, m := range mention.FindAllStringSubmatch(text, -1) {
					line := hintCommand(m[2])
					if line == "" || isProse(line) {
						continue
					}
					checked++
					if problem := unresolved(line); problem != "" {
						t.Errorf("%s: %q %s", fset.Position(lit.Pos()), line, problem)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no hints found; if the way they are written changed, this test has to change with it")
	}
}

// Sentences that land in a command's position anyway. Each is here because
// the test found it, and a new one should be as rare: an indented line in a
// multi-line message, and "run homebutler as root", which is about the binary
// rather than a subcommand.
var proseAfterName = []string{
	"homebutler will ", // internal/remote/ssh.go: the TOFU password refusal
	"homebutler as ",   // internal/watch/restart.go: run homebutler as root
}

func isProse(line string) bool {
	for _, prefix := range proseAfterName {
		if strings.HasPrefix(line+" ", prefix) {
			return true
		}
	}
	return false
}

// hintCommand trims a mention to the command it names: up to the end of a
// sentence, a comment, or a shell operator, with format verbs read as the
// placeholders they are.
func hintCommand(s string) string {
	for _, stop := range []string{". ", ", ", " #", " (", " && ", " || ", " 2>", " —", "; "} {
		if i := strings.Index(s, stop); i >= 0 {
			s = s[:i]
		}
	}
	s = strings.TrimRight(strings.TrimSpace(s), ".,:")
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.Contains(f, "%") {
			fields[i] = "<" + f + ">"
		}
	}
	return strings.Join(fields, " ")
}

// unresolved says what is wrong with a command line, or nothing.
func unresolved(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	if skillChild(rootCmd, fields[1]) == nil && !strings.HasPrefix(fields[1], "-") && !strings.HasPrefix(fields[1], "<") {
		return "starts with " + strconv.Quote(fields[1]) + ", which is not a homebutler command"
	}
	command, flags := resolve(rootCmd, line)
	switch {
	case command == nil:
		return "names a subcommand that does not exist"
	case command == rootCmd && !strings.HasPrefix(fields[1], "-") && !strings.HasPrefix(fields[1], "<"):
		return "starts with " + strconv.Quote(fields[1]) + ", which is not a homebutler command"
	}
	for _, flag := range flags {
		if command.Flag(flag) == nil {
			return "passes --" + flag + ", which " + strconv.Quote(command.CommandPath()) + " does not take"
		}
	}
	return ""
}
