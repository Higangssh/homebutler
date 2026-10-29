package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The secrets in testdata/top-secret-args*.txt: one passed as a flag value and
// one positional, which no flag-aware redaction could have caught.
var topFixtureSecrets = []string{"FAKE123", "hunter2-positional", "--token", "serve", "sleep"}

// fakeDockerTop puts a docker on PATH that answers `docker top <c> -o …`
// with the comm fixture and plain `docker top <c>` with the other, so Top
// runs its real path without a daemon. An empty comm fixture makes the -o
// call fail the way a ps that rejects the columns does.
func fakeDockerTop(t *testing.T, commFixture, plainFixture string) {
	t.Helper()
	path := func(name string) string {
		abs, err := filepath.Abs(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return abs
	}
	onO := "echo 'Error response from daemon: ps: unknown option'; exit 1"
	if commFixture != "" {
		onO = "cat '" + path(commFixture) + "'; exit 0"
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = -o ]; then " + onO + "; fi\ndone\ncat '" + path(plainFixture) + "'\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func marshalTop(t *testing.T) (*TopResult, string) {
	t.Helper()
	result, err := Top("probe")
	if err != nil {
		t.Fatalf("Top: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return result, string(data)
}

// docker top --json and the MCP tool both marshal TopResult. A command line
// carries whatever was passed on it, so the wire gets the kernel's name for
// the process and nothing else, as processes does.
func TestTopJSONCarriesOnlyTheExecutableName(t *testing.T) {
	fakeDockerTop(t, "top-secret-args-comm.txt", "top-secret-args.txt")

	result, data := marshalTop(t)
	for _, secret := range topFixtureSecrets {
		if strings.Contains(data, secret) {
			t.Errorf("JSON carries %q from the command line: %s", secret, data)
		}
	}
	if len(result.Processes) != 2 {
		t.Fatalf("got %d processes, want 2: %s", len(result.Processes), data)
	}
	if got := result.Processes[0].Command; got != "sh" {
		t.Errorf("first command = %q, want sh", got)
	}
	if got := result.Processes[1].Command; got != "probe" {
		t.Errorf("second command = %q, want probe", got)
	}
	if got, want := result.Processes[1].Invocation, "/usr/local/bin/probe serve --token=FAKE123 hunter2-positional"; got != want {
		t.Errorf("invocation = %q, want the full command line %q", got, want)
	}
}

// argv[0] is the process's to set. top-argv0*.txt are captures from a debian
// container running sleep under exec -a with the secret as argv[0], a
// bracketed title, a path ending in a slash, a zero-width space and a tab
// inside argv[0], and an empty argv[0] that moves the first argument to the
// front. Any name taken from the command line carries each of them.
func TestTopJSONIgnoresWhatArgv0Says(t *testing.T) {
	fakeDockerTop(t, "top-argv0-comm.txt", "top-argv0.txt")

	result, data := marshalTop(t)
	for _, secret := range []string{"XA1", "HOSTB2", "celery", "SECRETC3", "XD4", "XE5", "3601", "--token"} {
		if strings.Contains(data, secret) {
			t.Errorf("JSON carries %q from argv[0]: %s", secret, data)
		}
	}
	if len(result.Processes) != 7 || result.Skipped != 0 {
		t.Fatalf("got %d processes, %d skipped; want 7 and 0: %s", len(result.Processes), result.Skipped, data)
	}
	for _, p := range result.Processes[1:] {
		if p.Command != "sleep" {
			t.Errorf("pid %s command = %q, want sleep", p.PID, p.Command)
		}
	}
	if got := result.Processes[1].Invocation; got != "--token=XA1 3600" {
		t.Errorf("the table lost the command line: invocation = %q", got)
	}
}

// A ps that rejects the columns still gets a table. The name then comes from
// the command line, which is no more than the first word of what 1.3.0 put
// in the answer whole.
func TestTopFallsBackWhenPsRejectsColumns(t *testing.T) {
	fakeDockerTop(t, "", "top-secret-args.txt")

	result, data := marshalTop(t)
	if len(result.Processes) != 2 {
		t.Fatalf("got %d processes, want 2: %s", len(result.Processes), data)
	}
	if result.Processes[0].Command != "sh" || result.Processes[1].Command != "probe" {
		t.Errorf("fallback names = %q, %q; want sh, probe", result.Processes[0].Command, result.Processes[1].Command)
	}
	for _, secret := range topFixtureSecrets {
		if strings.Contains(data, secret) {
			t.Errorf("fallback JSON carries %q: %s", secret, data)
		}
	}
	if !strings.Contains(result.Processes[1].Invocation, "--token=FAKE123") {
		t.Errorf("fallback table lost the command line: %q", result.Processes[1].Invocation)
	}
}

func TestParseDockerTopCommRejectsOtherLayouts(t *testing.T) {
	for _, out := range []string{
		"",
		mustReadFixture(t, "top-linux.txt"),
		"USER PID COMM ARGS\nroot 1 sh sh\n",
	} {
		if _, _, ok := parseDockerTopComm(out); ok {
			t.Errorf("read a layout that is not PID USER comm args:\n%s", out)
		}
	}
	processes, skipped, ok := parseDockerTopComm("PID USER COMMAND COMMAND\n1 root sh\n2 root\n")
	if !ok || skipped != 1 || len(processes) != 1 || processes[0].Command != "sh" || processes[0].Invocation != "" {
		t.Errorf("got %+v skipped=%d ok=%v", processes, skipped, ok)
	}
}

func TestCommandName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/usr/local/bin/probe serve --token=FAKE123 hunter2", "probe"},
		{"sh -c sleep 3600", "sh"},
		{"nginx: worker process", "nginx"},
		{"postgres: checkpointer", "postgres"},
		{"[kworker/0:1-events]", "[kworker/0:1-events]"},
		{"[sh] <defunct>", "[sh]"},
		{"/run.sh", "run.sh"},
		{"./app --secret x", "app"},
		{"/", "/"},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		got := CommandName(tt.in)
		if got != tt.want {
			t.Errorf("CommandName(%q) = %q, want %q", tt.in, got, tt.want)
		}
		// A remote answer may already be reduced; reducing again must not
		// change it.
		if again := CommandName(got); again != got {
			t.Errorf("CommandName is not idempotent on %q: %q", got, again)
		}
	}
}
