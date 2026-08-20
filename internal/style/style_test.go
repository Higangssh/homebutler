package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLabelledBlockAlignsValues(t *testing.T) {
	out := LabelledBlock([]string{
		"CPU: 23.5% (4 cores)",
		"Public ports: 2",
		"Disk /: 47.0/128.0 GB (37%)",
	}, "   ")

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}

	// Every value should start at the same column, set by the widest label
	// ("Public ports") plus the two-space gutter.
	want := -1
	for _, line := range lines {
		col := strings.Index(line, strings.TrimLeft(strings.SplitN(line, "  ", 2)[1], " "))
		if want == -1 {
			want = col
		}
		if col != want {
			t.Errorf("value column = %d, want %d, in %q", col, want, line)
		}
	}
}

// Lines with no "Label: value" shape are prose and must survive untouched.
func TestLabelledBlockPassesThroughProse(t *testing.T) {
	out := LabelledBlock([]string{
		"Containers: 6 running",
		"No significant changes since last report.",
	}, "   ")

	if !strings.Contains(out, "   No significant changes since last report.\n") {
		t.Errorf("prose line was reformatted:\n%s", out)
	}
	if !strings.Contains(out, "Containers") {
		t.Errorf("labelled line missing:\n%s", out)
	}
}

// Only the first ": " separates label from value; later ones belong to the
// value, as in "CPU: 23.5%, Memory: 3.2 GB".
func TestLabelledBlockSplitsOnFirstSeparatorOnly(t *testing.T) {
	out := LabelledBlock([]string{"CPU: 23.5% (4 cores), Memory: 3.2/8.0 GB (40%)"}, "")

	if !strings.HasPrefix(out, "CPU") {
		t.Errorf("expected CPU label first, got %q", out)
	}
	if !strings.Contains(out, "Memory: 3.2/8.0 GB (40%)") {
		t.Errorf("second separator should stay in the value, got %q", out)
	}
}

func TestLabelledBlockEmpty(t *testing.T) {
	if got := LabelledBlock(nil, "   "); got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func TestSectionPadsToRuleWidth(t *testing.T) {
	out := Section("Current Status")

	if !strings.Contains(out, "Current Status") {
		t.Errorf("title missing from %q", out)
	}
	if w := lipgloss.Width(out); w != RuleWidth {
		t.Errorf("width = %d, want %d", w, RuleWidth)
	}
}

// A title wider than the rule must still produce a usable line rather than
// attempting a negative amount of padding.
func TestSectionHandlesOverlongTitle(t *testing.T) {
	out := Section(strings.Repeat("x", RuleWidth*2))

	if lipgloss.Width(out) <= RuleWidth {
		t.Errorf("expected the rule to grow with the title, got width %d", lipgloss.Width(out))
	}
	if !strings.HasSuffix(out, "───") {
		t.Errorf("expected a minimum trailing rule, got %q", out)
	}
}
