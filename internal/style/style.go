// Package style holds the shared terminal palette used by homebutler's
// human-readable command output.
//
// Colour is handled by lipgloss, which resolves the terminal's capabilities
// once at startup: it degrades truecolour to the nearest 256- or 16-colour
// match, honours NO_COLOR, and emits no escape sequences at all when stdout
// is not a terminal. Output piped to a file, a cron job, or CI therefore
// stays plain text without any explicit check here.
package style

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The accent is the project's brand colour, the same one used by the logo
// and the website.
var (
	Accent = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ADD8"))
	Title  = lipgloss.NewStyle().Bold(true)
	Label  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	Dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	OK     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	Warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	Fail   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// RuleWidth is the total width of a section rule. It stays inside an 80
// column terminal with room to spare.
const RuleWidth = 62

// Section renders a titled horizontal rule:
//
//	── Current Status ────────────────────────────
func Section(title string) string {
	head := "── " + title + " "
	pad := RuleWidth - lipgloss.Width(head)
	if pad < 3 {
		pad = 3
	}
	return Accent.Render(head + strings.Repeat("─", pad))
}

// LabelledBlock renders "Label: value" lines with their labels padded to a
// common width, so the values line up in a column. Lines with no ": "
// separator are printed unchanged, which keeps prose entries such as
// "No significant changes since last report." readable alongside them.
//
// Padding is measured on the unstyled label and appended after rendering,
// since escape sequences would otherwise be counted as visible width.
func LabelledBlock(lines []string, indent string) string {
	type row struct{ label, value string }

	rows := make([]row, 0, len(lines))
	width := 0
	for _, line := range lines {
		label, value, ok := strings.Cut(line, ": ")
		if !ok {
			rows = append(rows, row{value: line})
			continue
		}
		rows = append(rows, row{label: label, value: value})
		if w := lipgloss.Width(label); w > width {
			width = w
		}
	}

	var b strings.Builder
	for _, r := range rows {
		if r.label == "" {
			fmt.Fprintf(&b, "%s%s\n", indent, r.value)
			continue
		}
		pad := strings.Repeat(" ", width-lipgloss.Width(r.label))
		fmt.Fprintf(&b, "%s%s%s  %s\n", indent, Label.Render(r.label), pad, r.value)
	}
	return b.String()
}
