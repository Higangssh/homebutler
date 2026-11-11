package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

func answeringServer(t *testing.T, stdout, stderr string, err error) {
	t.Helper()
	saved := remoteRunSplit
	t.Cleanup(func() { remoteRunSplit = saved })
	remoteRunSplit = func(_ *config.ServerConfig, _ ...string) ([]byte, []byte, error) {
		if err != nil {
			return nil, nil, err
		}
		return []byte(stdout), []byte(stderr), nil
	}
}

// `backup drill web --server pi --json` printed nothing and failed with the
// DrillResult inside the error message, where running it on the server
// prints the result and then fails. doctor --strict --server did the same
// with a warning.
func TestServerPrintsTheAnswerBeforeAFailingVerdict(t *testing.T) {
	const doc = `{"schema_version":1,"status":"warn"}` + "\n"
	answeringServer(t, "", "", &remote.ExitError{
		Status: 1,
		Stdout: []byte(doc),
		Stderr: []byte("error: doctor found 1 warning(s) and 0 failure(s)\n"),
		Err:    errors.New("[pi] remote command failed: Process exited with status 1\n  → Output: error: doctor found 1 warning(s) and 0 failure(s)\n  → Check if homebutler is installed on the remote server: homebutler deploy pi"),
	})

	var out, errOut bytes.Buffer
	err := passThroughTo(&out, &errOut, &config.ServerConfig{Name: "pi"}, []string{"doctor", "--strict", "--json"})
	if out.String() != doc {
		t.Fatalf("stdout = %q, want the remote's answer", out.String())
	}
	if err == nil {
		t.Fatal("the failing verdict exited 0")
	}
	if got := err.Error(); got != "[pi] doctor found 1 warning(s) and 0 failure(s)" {
		t.Fatalf("error = %q, want the remote's own sentence", got)
	}
}

// Stderr from a remote that succeeded goes to stderr. It used to be printed
// into stdout, ahead of the JSON.
func TestServerKeepsTheStreamsApart(t *testing.T) {
	answeringServer(t, `[]`+"\n", "warning: deprecated\n", nil)
	var out, errOut bytes.Buffer
	if err := passThroughTo(&out, &errOut, &config.ServerConfig{Name: "pi"}, []string{"notify", "test", "--json"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" || errOut.String() != "warning: deprecated\n" {
		t.Fatalf("stdout %q, stderr %q", out.String(), errOut.String())
	}
}

// A remote that printed nothing keeps the explanation Run gives, deploy hint
// and all, and a failure that is not a remote exit is passed on unchanged.
func TestServerFailuresWithoutAnAnswerAreUnchanged(t *testing.T) {
	missing := &remote.ExitError{
		Status: 127,
		Stderr: []byte("bash: homebutler: command not found\n"),
		Err:    errors.New("[pi] remote command failed: Process exited with status 127\n  → Output: bash: homebutler: command not found\n  → Check if homebutler is installed on the remote server: homebutler deploy pi"),
	}
	refused := errors.New("[pi] connection refused")
	for _, want := range []error{missing, refused} {
		answeringServer(t, "", "", want)
		var out, errOut bytes.Buffer
		err := passThroughTo(&out, &errOut, &config.ServerConfig{Name: "pi"}, []string{"status"})
		if !errors.Is(err, want) || out.Len() != 0 {
			t.Fatalf("err %v, stdout %q; want %v and nothing printed", err, out.String(), want)
		}
		if want == missing && !strings.Contains(err.Error(), "homebutler deploy pi") {
			t.Fatalf("lost the deploy hint: %v", err)
		}
	}
}
