package doctor

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The README tells a caller it can filter on a finding's category. That is a
// promise about this package, and the table in the README is where somebody
// integrating reads it — so the list is checked against that table rather than
// against a copy written here. A copy would agree with the code and say
// nothing about whether the documented promise still holds.
//
// The same shape as TestKindsMatchTheReadme in internal/report, for the same
// reason.
func TestCategoriesMatchTheReadme(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("the README is where the categories are documented: %v", err)
	}

	const header = "| Category | What it is about |"
	start := strings.Index(string(readme), header)
	if start < 0 {
		t.Fatal("no category table found in the README; if it moved, this test has to move with it")
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
		t.Fatal("the category table has no rows this test can read")
	}

	got := append([]string(nil), Categories()...)
	sort.Strings(got)
	sort.Strings(documented)

	if strings.Join(got, ",") != strings.Join(documented, ",") {
		t.Fatalf("the categories and the README disagree:\n  code:   %v\n  README: %v\n"+
			"A category a caller can receive and cannot look up is what this test exists to stop.", got, documented)
	}
}

// And every finding a run can produce carries one of them. A category invented
// at a call site reaches a caller that has no branch for it, which is the way
// the list grew to include a word describing our own collectors rather than
// anything about the machine being diagnosed.
func TestEveryFindingCarriesADocumentedCategory(t *testing.T) {
	known := map[string]bool{}
	for _, category := range Categories() {
		known[category] = true
	}

	for _, source := range goFiles(t) {
		for _, category := range categoriesIn(source) {
			if !known[category] {
				t.Errorf("a finding is added with category %q, which the README does not document", category)
			}
		}
	}
}

func goFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(data))
	}
	return out
}

// categoriesIn finds the category argument of every r.add call and every
// Finding literal, which are the two ways a finding comes into existence.
func categoriesIn(source string) []string {
	var out []string
	add := regexp.MustCompile(`r\.add\(Severity[A-Za-z]+, "([a-z]+)"`)
	for _, m := range add.FindAllStringSubmatch(source, -1) {
		out = append(out, m[1])
	}
	literal := regexp.MustCompile(`Category:\s*"([a-z]+)"`)
	for _, m := range literal.FindAllStringSubmatch(source, -1) {
		out = append(out, m[1])
	}
	return out
}
