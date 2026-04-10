package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Tab styles
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Padding(0, 2)

	// Panel styles
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)

	// Progress bar colors
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// Text styles
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))

	// Status styles
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	criticalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// Bar characters
	barFull  = "\u2588"
	barEmpty = "\u2591"
)

// progressBar renders a colorful progress bar of the given width.
// Color transitions: green (<70%) -> yellow (70-90%) -> red (>=90%).
func progressBar(percent float64, width int) string {
	if width < 5 {
		width = 5
	}
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	var style lipgloss.Style
	switch {
	case percent >= 90:
		style = redStyle
	case percent >= 70:
		style = yellowStyle
	default:
		style = greenStyle
	}

	bar := style.Render(strings.Repeat(barFull, filled)) +
		dimStyle.Render(strings.Repeat(barEmpty, width-filled))
	return bar
}

// alertStyle returns the appropriate style for an alert status.
func alertStyle(status string) lipgloss.Style {
	switch status {
	case "ok":
		return okStyle
	case "warning":
		return warningStyle
	case "critical":
		return criticalStyle
	default:
		return dimStyle
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "~"
}
