package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxHistory = 60

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// sparkline renders a mini graph from historical percentage data (0-100).
// width limits the maximum number of characters displayed.
// Partial data (len < width) renders left-aligned, growing from the left.
func sparkline(data []float64, width int) string {
	if len(data) == 0 || width <= 0 {
		return ""
	}

	start := 0
	if len(data) > width {
		start = len(data) - width
	}
	visible := data[start:]

	var b strings.Builder
	for _, v := range visible {
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		idx := int(v / 100.0 * 7.0)
		if idx > 7 {
			idx = 7
		}
		b.WriteRune(sparkBlocks[idx])
	}

	return b.String()
}

// sparklineColor returns the appropriate style for a sparkline
// based on the last value: green <50%, yellow 50-80%, red >80%.
func sparklineColor(data []float64) lipgloss.Style {
	if len(data) == 0 {
		return greenStyle
	}
	last := data[len(data)-1]
	switch {
	case last > 80:
		return redStyle
	case last >= 50:
		return yellowStyle
	default:
		return greenStyle
	}
}

// appendHistory adds a value to a history slice, capping at maxHistory entries.
func appendHistory(history []float64, value float64) []float64 {
	history = append(history, value)
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	return history
}
