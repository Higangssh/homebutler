// Package termbg stops bubbletea asking the terminal for its background
// colour at start-up.
//
// bubbletea's init asks lipgloss whether the background is dark, which
// writes an OSC 11 query to the terminal and waits up to five seconds for an
// answer, in every command and not only the TUI. Nothing in homebutler is an
// AdaptiveColor, so the answer is never read; settling it here makes the
// query a no-op.
//
// That only works if this init runs before bubbletea's. Go initialises the
// first package, in import-path order, whose imports are all initialised.
// This path sorts before github.com/charmbracelet/bubbletea, and lipgloss is
// the only import, so it is ready as soon as bubbletea is. Import anything
// else here and bubbletea may win the race. internal/tui, the one package
// that imports bubbletea, imports this one, and
// TestStartupDoesNotQueryTheTerminal in package main checks the result.
package termbg

import "github.com/charmbracelet/lipgloss"

func init() {
	lipgloss.SetHasDarkBackground(true)
}
