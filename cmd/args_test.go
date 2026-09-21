package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Cobra accepts anything by default. A command with no subcommands and no
// Args field takes whatever it is given, and nothing ever looks at it — so
// `homebutler report not-a-thing` printed a report and exited 0, and
// `homebutler mcp report` did nothing and exited 0. An unknown flag was
// caught; an unknown word was swallowed.
//
// That is the shape the 0.37.0 headline was about, one layer down: the exit
// code is what a script reads, and it said the command had worked.
//
// A validator someone forgets on a command added in six months is not a fix,
// which is why this walks the tree rather than listing what exists today.
func TestEveryLeafCommandSaysWhatArgumentsItTakes(t *testing.T) {
	var missing []string

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		// A command with subcommands is dispatched by cobra, which already
		// rejects a name it does not have.
		if len(c.Commands()) == 0 && c.Args == nil {
			missing = append(missing, c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these commands accept any argument and read none of it: %v\n"+
			"Give each one an Args validator — cobra.NoArgs when it takes nothing, "+
			"or the ExactArgs/RangeArgs that matches what it reads.", missing)
	}
}

// The other half: a command that says it takes an argument has to be one that
// reads one. `Use: "restart <container>"` with cobra.NoArgs would reject the
// thing its own usage line asks for.
func TestUsageLineAgreesWithTheValidator(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if len(c.Commands()) == 0 && c.Args != nil {
			takesNone := c.Args(c, []string{}) == nil && c.Args(c, []string{"x"}) != nil
			declaresOne := len(splitUse(c.Use)) > 1
			if takesNone && declaresOne {
				t.Errorf("%s rejects every argument and its usage line asks for one: %q", c.CommandPath(), c.Use)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

func splitUse(use string) []string { return strings.Fields(use) }
