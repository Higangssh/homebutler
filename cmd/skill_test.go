package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/doctor"
)

// skills/SKILL.md is published to ClawHub, where it is what an agent reads
// before running anything. It had drifted eight releases: it described `watch`
// as a terminal dashboard, `serve` as read-only, and `trust` as always
// required. An agent following it would run commands that do not do what it
// was told, or do not exist.
//
// So every command in it is resolved against the binary's own command tree.
// The tree is the source of truth, and a skill naming something that is not in
// it fails the build rather than an agent's afternoon.
func TestEveryCommandInTheSkillExists(t *testing.T) {
	skill, err := os.ReadFile("../skills/SKILL.md")
	if err != nil {
		t.Fatalf("the skill is part of what ships: %v", err)
	}

	lines := commandLines(string(skill))
	if len(lines) == 0 {
		t.Fatal("no homebutler commands found in the skill; if the format changed, this test has to change with it")
	}

	for _, line := range lines {
		command, flags := resolve(rootCmd, line)
		if command == nil {
			t.Errorf("%q names a subcommand that does not exist", line)
			continue
		}
		if command == rootCmd && len(strings.Fields(line)) > 1 && !strings.HasPrefix(strings.Fields(line)[1], "--") {
			// The root takes no arguments, so a first word that did not
			// resolve is a command that is not there.
			t.Errorf("%q starts with %q, which is not a homebutler command", line, strings.Fields(line)[1])
			continue
		}
		for _, flag := range flags {
			if command.Flag(flag) == nil {
				t.Errorf("%q passes --%s, which %q does not take", line, flag, command.CommandPath())
			}
		}
	}
}

// commandLines pulls every `homebutler …` line out of the fenced blocks. Prose
// mentioning a command in backticks is not run by anybody; a line in a block is.
func commandLines(markdown string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inBlock = !inBlock
			continue
		}
		if !inBlock {
			continue
		}
		line = strings.TrimSpace(line)
		if comment := strings.Index(line, " #"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if strings.HasPrefix(line, "homebutler ") || line == "homebutler" {
			out = append(out, line)
		}
	}
	return out
}

// resolve walks the command tree the way cobra would, and collects the long
// flags the line passes. A token in angle brackets is a placeholder for the
// reader, and anything else that is not a subcommand is an argument.
func resolve(root *cobra.Command, line string) (*cobra.Command, []string) {
	fields := strings.Fields(line)
	current := root
	var flags []string
	walking := true

	for _, token := range fields[1:] {
		switch {
		case strings.HasPrefix(token, "--"):
			walking = false
			name := strings.TrimPrefix(token, "--")
			if equals := strings.Index(name, "="); equals >= 0 {
				name = name[:equals]
			}
			flags = append(flags, name)
		case !walking || strings.HasPrefix(token, "<") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "$"):
			walking = false
		default:
			if child := skillChild(current, token); child != nil {
				current = child
				continue
			}
			// A command that only dispatches — `watch`, `backup list`'s parent
			// — takes no arguments of its own, so a word that is not one of its
			// subcommands is a subcommand that does not exist.
			if current.HasSubCommands() && !current.Runnable() {
				return nil, nil
			}
			// Otherwise the rest is arguments.
			walking = false
		}
	}
	return current, flags
}

func skillChild(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

// The skill lists the MCP tools an agent can call, and that list is the half
// that actually drifted: ten were missing, including doctor and notify_test —
// the two an agent most needs to know it has. The registry is where tools are
// declared, so the list is checked against it rather than against a count
// somebody remembered to update.
//
// Only one direction is checked. A tool the registry has and the skill does
// not name is a capability an agent does not know about; a backticked word
// that is not a tool is just a word.
func TestTheSkillNamesEveryTool(t *testing.T) {
	skill, err := os.ReadFile("../skills/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}

	named := map[string]bool{}
	for i, field := range strings.Split(string(skill), "`") {
		if i%2 == 1 {
			named[strings.TrimSpace(field)] = true
		}
	}

	missing := 0
	for _, c := range capability.Registry {
		if !named[c.Tool.Name] {
			t.Errorf("the registry has %q and the skill never names it; an agent reading this does not know it exists", c.Tool.Name)
			missing++
		}
	}
	if missing == 0 && len(named) == 0 {
		t.Fatal("nothing backticked in the skill; if the format changed, this test has to change with it")
	}
}

// A pinned version is the right answer to "do not install an unpinned
// executable", and it is wrong the moment it is left behind. The versions in
// the install section have to be the newest release the changelog names.
//
// Versions elsewhere in the file are deliberate history — "since 0.34.0, a
// password server must be trusted" — so only the install section is checked.
func TestTheSkillInstallsTheCurrentVersion(t *testing.T) {
	skill, err := os.ReadFile("../skills/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}

	released := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindStringSubmatch(string(changelog))
	if released == nil {
		t.Fatal("no released version found in the changelog")
	}
	current := released[1]

	section := string(skill)
	if start := strings.Index(section, "## Prerequisites"); start >= 0 {
		section = section[start:]
	} else {
		t.Fatal("the skill has no Prerequisites section; if it moved, this test has to move with it")
	}

	// The finding this pinning answers is "unpinned executable dependency", so
	// a version that went back to a moving tag has to fail even though nothing
	// in the section is then out of date.
	if strings.Contains(section, "latest") {
		t.Errorf("the install section names a moving tag:\n%s", firstLineWith(section, "latest"))
	}

	found := regexp.MustCompile(`v?(\d+\.\d+\.\d+)`).FindAllStringSubmatch(section, -1)
	if len(found) == 0 {
		t.Fatal("the install section pins nothing; an agent installing an unpinned executable cannot say what it ran")
	}
	for _, match := range found {
		if match[1] != current {
			t.Errorf("the install section pins %s and the current release is %s — update the pin in skills/SKILL.md, which is part of cutting a release",
				match[1], current)
		}
	}
}

func firstLineWith(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// "What needs a shell" is a claim about the registry, not a preference: the
// skill says shell commands are for the things no tool exposes. `homebutler
// notify test` sat in that list while `notify_test` was both an MCP tool and a
// dashboard button, so an agent was sent to a shell for something it could
// call. The claim is checkable, so it is checked — reading the list against
// the classifier rather than against a copy of it.
func TestTheShellListNamesNothingAToolExposes(t *testing.T) {
	skill, err := os.ReadFile("../skills/SKILL.md")
	if err != nil {
		t.Fatalf("the skill is part of what ships: %v", err)
	}

	lines := commandLines(sectionOf(string(skill), "## What needs a shell"))
	if len(lines) == 0 {
		t.Fatal("no commands found under 'What needs a shell'; if the heading moved, this test has to move with it")
	}

	for _, line := range lines {
		if runner, tool := doctor.ClassifyCommand(line); runner == doctor.RunnerMCP {
			t.Errorf("%q is listed as needing a shell, and %s exposes it", line, tool)
		}
	}
}

// sectionOf returns the markdown from heading up to the next heading of the
// same level, so a list is read with the sentence that introduced it.
func sectionOf(markdown, heading string) string {
	start := strings.Index(markdown, heading)
	if start < 0 {
		return ""
	}
	rest := markdown[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}
