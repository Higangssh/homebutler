package report

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
)

// The README tells an agent that the kind is one of eight words and that it is
// the same word in --json, so it can branch without reading prose. That is a
// promise about this package, and the table in the README is where somebody
// integrating reads it.
//
// The set is checked against that table rather than against a copy written
// here: a copy would agree with the code and say nothing about whether the
// promise still holds.
func TestKindsMatchTheReadme(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("the README is where the kinds are documented: %v", err)
	}

	// The README has several tables; this is the one headed by Kind. Rows are
	// read from it until the table ends, so the app list further down is not
	// mistaken for a list of change kinds.
	const header = "| Kind | Means | You would see it after |"
	start := strings.Index(string(readme), header)
	if start < 0 {
		t.Fatal("no kind table found in the README; if it moved, this test has to move with it")
	}

	row := regexp.MustCompile("^\\| `([a-z]+)` \\|")
	var documented []string
	for _, line := range strings.Split(string(readme)[start:], "\n")[1:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		if match := row.FindStringSubmatch(line); match != nil {
			documented = append(documented, match[1])
		}
	}
	if len(documented) == 0 {
		t.Fatal("the kind table has no rows this test can read")
	}

	got := Kinds()
	sort.Strings(got)
	sort.Strings(documented)

	if strings.Join(got, ",") != strings.Join(documented, ",") {
		t.Fatalf("the kinds and the README disagree:\n  code:   %v\n  README: %v\n"+
			"A kind an agent can receive and cannot look up is the thing this test exists to stop.", got, documented)
	}
	if len(got) != 8 {
		t.Fatalf("the README says eight kinds and there are %d: %v", len(got), got)
	}
}

// Every kind a change can carry is one of those words. A kind invented at a
// call site would reach an agent that has no branch for it.
func TestEveryChangeCarriesADocumentedKind(t *testing.T) {
	known := map[string]bool{}
	for _, kind := range Kinds() {
		known[kind] = true
	}

	prev := snapshotWith([]docker.Container{
		{ID: "aaa", Name: "vaultwarden", Image: "vaultwarden:1.32", State: "running"},
	}, nil)
	curr := snapshotWith([]docker.Container{
		{ID: "bbb", Name: "vaultwarden", Image: "vaultwarden:1.33", State: "exited"},
		{ID: "ccc", Name: "db", Image: "postgres:16", State: "running"},
	}, nil)

	report := buildReport(curr, prev)
	if len(report.NotableChanges) == 0 {
		t.Fatal("the fixture produced no changes, so this test checks nothing")
	}
	for _, line := range report.NotableChanges {
		if !known[line.Kind] {
			t.Errorf("change %q carries kind %q, which the README does not document", line.Text, line.Kind)
		}
		if line.Text == "" {
			t.Errorf("change of kind %q has no text for a person to read", line.Kind)
		}
	}
}
