package contract

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden file with the current surface")

const goldenPath = "testdata/surface.golden"

// The surface is compared byte for byte against a file in the repository.
//
// The point is not that the surface cannot change — it is that changing it
// cannot happen quietly. A renamed tool, a retyped field, a risk class that
// moved, a route that stopped needing a token: each of them shows up here as a
// diff in a pull request, next to the changelog line that has to explain it.
//
// Regenerate deliberately:
//
//	go test ./internal/contract -update
func TestTheFrozenSurfaceHasNotMoved(t *testing.T) {
	current := Surface()

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(current), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden file rewritten; commit it with the change that moved the surface")
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("the golden file is the contract: %v", err)
	}
	if string(want) == current {
		return
	}

	t.Errorf("the frozen surface changed.\n\n%s\n\n"+
		"If that was deliberate: regenerate with `go test ./internal/contract -update`, commit the\n"+
		"golden file with the change, and write the ⚠️ Behavior changes line. docs/compatibility.md\n"+
		"says what may change without a major release and what may not.",
		diff(string(want), current))
}

// diff prints the lines that were added and removed rather than both files. A
// surface this size scrolls a terminal off the screen otherwise, and what
// matters is the three lines that moved.
func diff(want, got string) string {
	inWant := map[string]bool{}
	for _, line := range strings.Split(want, "\n") {
		inWant[line] = true
	}
	inGot := map[string]bool{}
	for _, line := range strings.Split(got, "\n") {
		inGot[line] = true
	}

	var b strings.Builder
	for _, line := range strings.Split(want, "\n") {
		if line != "" && !inGot[line] {
			b.WriteString("  - " + line + "\n")
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if line != "" && !inWant[line] {
			b.WriteString("  + " + line + "\n")
		}
	}
	if b.Len() == 0 {
		return "  (the lines are the same; the ordering or the trailing newline changed)"
	}
	return b.String()
}

// The vocabularies are named in documentation an agent reads, so they are
// frozen as sets rather than as whatever the code happens to return.
func TestTheVocabulariesAreTheDocumentedOnes(t *testing.T) {
	surface := Surface()
	for _, want := range []string{
		// In the order the README documents them, which is by how much an
		// operator can act on them — not alphabetical.
		"report.kind: gone new replaced image state port disk skipped",
		"doctor.runner: mcp cli shell",
	} {
		if !strings.Contains(surface, want) {
			t.Errorf("the surface no longer carries %q", want)
		}
	}
}
