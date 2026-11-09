package cmd

import (
	"io"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// What each command does with the global --server and --all, written out here
// rather than read from the code under test, so moving a command between the
// two is a decision this file has to be told about.
var (
	takesServer = []string{
		"status", "alerts", "ports", "processes", "report", "doctor", "wake",
		"network scan",
		"docker list", "docker restart", "docker stop", "docker logs",
		"docker stats", "docker top", "docker inspect",
		"backup", "backup list", "backup drill", "restore",
		"deploy",
	}
	takesAll = append(slices.Clone(takesServer), "upgrade")
)

// runnableCommands is the path below the root of every command that runs
// something, leaving out help and completion, which describe the CLI.
func runnableCommands() []*cobra.Command {
	var out []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c != rootCmd && c.Runnable() {
			switch strings.SplitN(commandPathOf(c), " ", 2)[0] {
			case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			default:
				out = append(out, c)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	return out
}

func commandPathOf(c *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), "homebutler ")
}

// stubRuns swaps every command's Run for one that records which command ran,
// and its Args for one that takes anything, so the whole tree can be executed
// without any of it touching this machine.
func stubRuns(t *testing.T) *string {
	t.Helper()
	var ran string
	type saved struct {
		run  func(*cobra.Command, []string)
		runE func(*cobra.Command, []string) error
		args cobra.PositionalArgs
	}
	originals := map[*cobra.Command]saved{}
	for _, c := range runnableCommands() {
		originals[c] = saved{c.Run, c.RunE, c.Args}
		c.Run = nil
		c.RunE = func(cmd *cobra.Command, _ []string) error {
			ran = commandPathOf(cmd)
			return nil
		}
		c.Args = cobra.ArbitraryArgs
	}
	t.Cleanup(func() {
		for c, s := range originals {
			c.Run, c.RunE, c.Args = s.run, s.runE, s.args
		}
	})
	return &ran
}

func executeRoot(t *testing.T, ran *string, args ...string) error {
	t.Helper()
	*ran = ""
	serverName, allServers = "", false
	rootCmd.SetArgs(args)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	t.Cleanup(func() {
		serverName, allServers = "", false
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	return rootCmd.Execute()
}

// `install jellyfin --server rpi5` installed jellyfin on this machine and
// `inventory scan --server rpi5` answered about this machine, because only
// the commands that call maybeRouteRemote ever looked at either flag. A
// command that cannot reach another server has to say so before it runs.
// This walks the tree, so a command added later is held to it too.
func TestCommandsThatIgnoreServerOrAllRefuseThem(t *testing.T) {
	ran := stubRuns(t)

	forms := []struct {
		flag  string
		args  []string
		takes []string
	}{
		{"--server", []string{"--server", "rpi5"}, takesServer},
		{"--server", []string{"--server=rpi5"}, takesServer},
		{"--all", []string{"--all"}, takesAll},
	}

	for _, c := range runnableCommands() {
		path := commandPathOf(c)
		for _, form := range forms {
			args := append(strings.Fields(path), form.args...)
			err := executeRoot(t, ran, args...)
			if slices.Contains(form.takes, path) {
				if err != nil || *ran != path {
					t.Errorf("homebutler %s: want it to run, got ran=%q err=%v", strings.Join(args, " "), *ran, err)
				}
				continue
			}
			if *ran != "" {
				t.Errorf("homebutler %s: ran %s, want it refused before running", strings.Join(args, " "), *ran)
				continue
			}
			if err == nil || !strings.HasPrefix(err.Error(), path+" ") || !strings.Contains(err.Error(), form.flag+" is not supported") {
				t.Errorf("homebutler %s: error %v, want one naming %q and %s", strings.Join(args, " "), err, path, form.flag)
			}
		}
	}
}

func TestBothFlagsAreNamedWhenBothAreRefused(t *testing.T) {
	ran := stubRuns(t)
	err := executeRoot(t, ran, "install", "jellyfin", "--dry-run", "--server", "rpi5", "--all")
	if err == nil || err.Error() != "install runs on this machine only; --server and --all are not supported" {
		t.Fatalf("err = %v", err)
	}
}

// `backup drill --all` is drill's own flag, every app in one archive, and it
// shadows the global one. It must not be mistaken for a request to fan out.
func TestBackupDrillsOwnAllIsNotTheGlobalOne(t *testing.T) {
	ran := stubRuns(t)
	if err := executeRoot(t, ran, "backup", "drill", "--all"); err != nil || *ran != "backup drill" {
		t.Fatalf("ran=%q err=%v", *ran, err)
	}
	if allServers {
		t.Fatal("backup drill --all set the global --all")
	}
}

func TestFlagsThatAskForNothingAreNotRefused(t *testing.T) {
	ran := stubRuns(t)
	for _, args := range [][]string{
		{"install", "list", "--all=false"},
		{"inventory", "scan", "--server="},
	} {
		if err := executeRoot(t, ran, args...); err != nil {
			t.Errorf("homebutler %s: %v", strings.Join(args, " "), err)
		}
	}
}

func TestHelpAndCompletionIgnoreServer(t *testing.T) {
	ran := stubRuns(t)
	for _, args := range [][]string{
		{"help", "install", "--server", "rpi5"},
		{"install", "--help", "--server", "rpi5"},
		{"completion", "bash", "--all"},
	} {
		if err := executeRoot(t, ran, args...); err != nil {
			t.Errorf("homebutler %s: %v", strings.Join(args, " "), err)
		}
		if *ran != "" {
			t.Errorf("homebutler %s ran %s", strings.Join(args, " "), *ran)
		}
	}
}

// Every command named as taking a flag has to exist; a renamed one would
// otherwise drop out of the list and start refusing what it supports.
func TestEveryCommandNamedAsTakingAFlagExists(t *testing.T) {
	var paths []string
	for _, c := range runnableCommands() {
		paths = append(paths, commandPathOf(c))
	}
	sort.Strings(paths)
	for _, p := range takesAll {
		if !slices.Contains(paths, p) {
			t.Errorf("%q is not a command", p)
		}
	}
}
