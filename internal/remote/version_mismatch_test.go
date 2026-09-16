package remote

import (
	"errors"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// A remote command runs the remote binary, and when that binary predates the
// command it is asked for, the only thing said used to be "unknown command"
// with a hint to check an install that is already there.
//
// This is easy to walk into. The commands that existed back then still work,
// so the setup looks correct until the first one that did not — and then the
// failure looks like a missing install rather than an old one.
func TestAnOlderRemoteNamesBothVersions(t *testing.T) {
	old := LocalVersion
	LocalVersion = "0.35.1"
	t.Cleanup(func() { LocalVersion = old })

	server := &config.ServerConfig{Name: "pi", Host: "192.168.0.4"}
	out := []byte("error: unknown command: report (run 'homebutler help' for usage)")

	err := remoteCommandError(server, "0.8.0", []string{"report", "--json"}, out, errors.New("Process exited with status 1"))

	for _, want := range []string{"0.8.0", "0.35.1", "homebutler upgrade", "report"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the failure does not mention %q: %v", want, err)
		}
	}
	// The old hint sent someone to check an install that is already there.
	if strings.Contains(err.Error(), "homebutler deploy") {
		t.Fatalf("still points at deploy: %v", err)
	}
	if class := Classify(err); class != ClassRemote {
		t.Fatalf("classed %q, want %q", class, ClassRemote)
	}
}

// Nothing installed at all is a different problem with a different answer, and
// it keeps the answer it had.
func TestARemoteWithNoHomebutlerStillSaysDeploy(t *testing.T) {
	server := &config.ServerConfig{Name: "pi", Host: "192.168.0.4"}
	out := []byte("bash: homebutler: command not found")

	// No version came back, because there is nothing there to ask.
	err := remoteCommandError(server, "", []string{"report"}, out, errors.New("Process exited with status 127"))

	if !strings.Contains(err.Error(), "homebutler deploy pi") {
		t.Fatalf("a missing install should point at deploy: %v", err)
	}
	if strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("a missing install should not point at upgrade: %v", err)
	}
}

// A command that failed for its own reasons is not a version problem, whatever
// the far side is running.
func TestAFailureThatIsNotAboutTheVersionIsUnchanged(t *testing.T) {
	server := &config.ServerConfig{Name: "pi", Host: "192.168.0.4"}
	out := []byte("error: docker daemon is not running")

	err := remoteCommandError(server, "0.8.0", []string{"docker", "list"}, out, errors.New("Process exited with status 1"))

	if strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("an unrelated failure was explained as a version mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), "docker daemon is not running") {
		t.Fatalf("the remote's own output is gone: %v", err)
	}
}

// Without a local version the explanation is shorter rather than wrong.
func TestTheExplanationSurvivesAnUnknownLocalVersion(t *testing.T) {
	old := LocalVersion
	LocalVersion = ""
	t.Cleanup(func() { LocalVersion = old })

	err := remoteCommandError(&config.ServerConfig{Name: "pi"}, "0.8.0", []string{"report"},
		[]byte("error: unknown command: report"), errors.New("exit 1"))

	if !strings.Contains(err.Error(), "0.8.0") || !strings.Contains(err.Error(), "homebutler upgrade") {
		t.Fatalf("the explanation lost its point: %v", err)
	}
}
