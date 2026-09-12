package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every command the wizard offers has to exist. `homebutler tui` was offered
// here for six months without ever being one (#159): the TUI was `homebutler
// watch` when the wizard was written and is `homebutler watch tui` now, and the
// suggestion matched neither.
func TestNextStepsResolve(t *testing.T) {
	for _, step := range nextSteps {
		for _, command := range step.commands {
			if _, ok := resolveCommand(rootCmd, strings.Fields(command)); !ok {
				t.Errorf("init offers %q, which is not a command", "homebutler "+command)
			}
		}
	}
}

// The check is only worth having if it fails on the thing it was written for.
func TestResolveCommandRejectsUnknownCommand(t *testing.T) {
	if _, ok := resolveCommand(rootCmd, []string{"tui"}); ok {
		t.Fatal("bare tui resolved; it has never been a command")
	}
}

// resolveCommand walks the command tree, stopping at the first placeholder
// argument such as <container>, which is an argument rather than a subcommand.
func resolveCommand(root *cobra.Command, path []string) (*cobra.Command, bool) {
	current := root
	for _, name := range path {
		if strings.HasPrefix(name, "<") || strings.HasPrefix(name, "[") {
			return current, true
		}
		child, ok := childNamed(current, name)
		if !ok {
			return nil, false
		}
		current = child
	}
	return current, true
}

func childNamed(parent *cobra.Command, name string) (*cobra.Command, bool) {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child, true
		}
		for _, alias := range child.Aliases {
			if alias == name {
				return child, true
			}
		}
	}
	return nil, false
}
