package remote

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// streamsServer answers each homebutler command the way a release does: the
// answer on stdout, the reason on stderr, and the exit status beside them.
func streamsServer(t *testing.T) *config.ServerConfig {
	t.Helper()
	server := startTestSSHServerStreams(t, func(cmd string) (string, string, uint32) {
		switch {
		case strings.Contains(cmd, "homebutler version"):
			return "homebutler 1.5.0 (built x)\n", "", 0
		case strings.Contains(cmd, "homebutler notify test --json"):
			// A legacy alerts.yaml makes it warn on stderr before answering.
			return `[{"channel":"telegram","sent":true}]` + "\n", "warning: ~/.homebutler/alerts.yaml is deprecated\n", 0
		case strings.Contains(cmd, "homebutler backup drill web --json"):
			return `{"app":"web","passed":false,"error":"no backup found"}` + "\n", "error: web did not come back from the backup\n", 1
		case strings.Contains(cmd, "homebutler backup drill text"):
			return "Drill: web\n  ❌ failed\n", "error: web did not come back from the backup\n", 1
		case strings.Contains(cmd, "homebutler missing"):
			return "", "bash: line 1: homebutler: command not found\n", 127
		}
		return "", "error: unknown command\n", 1
	})
	t.Cleanup(server.cleanup)
	writeKnownHostsForTest(t, server)

	host, portStr, err := net.SplitHostPort(server.addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return &config.ServerConfig{Name: "pi", Host: host, Port: port, User: "tester", AuthMode: "password", Password: "secret"}
}

// stderr was part of what came back, so a warning the remote printed beside
// its answer made the answer invalid JSON.
func TestRunReturnsStdoutAlone(t *testing.T) {
	srv := streamsServer(t)

	out, err := Run(srv, "notify", "test", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `[{"channel":"telegram","sent":true}]` {
		t.Fatalf("Run = %q, want the answer alone", got)
	}

	_, errOut, err := RunSplit(srv, "notify", "test", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(errOut), "deprecated") {
		t.Fatalf("RunSplit lost stderr: %q", errOut)
	}
}

// A drill that failed printed its result and then exited 1. The result is
// what the command exists to give, and it used to be lost in the error.
func TestAnAnswerBeforeAFailingExitIsKept(t *testing.T) {
	srv := streamsServer(t)

	out, err := Run(srv, "backup", "drill", "web", "--json")
	if err == nil {
		t.Fatalf("a failing exit came back as success: %s", out)
	}
	answer, ok := Answered(err)
	if !ok || !strings.Contains(string(answer), `"passed":false`) {
		t.Fatalf("Answered = %q, %v; want the DrillResult", answer, ok)
	}
	// Still an error, with the remote's reason in it and not the JSON.
	if !strings.Contains(err.Error(), "web did not come back from the backup") {
		t.Fatalf("stderr is not in the error: %v", err)
	}
	if strings.Contains(err.Error(), `"passed"`) {
		t.Fatalf("the answer was dumped into the error: %v", err)
	}
	if Classify(err) != ClassRemote {
		t.Fatalf("classed %q", Classify(err))
	}
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Status != 1 {
		t.Fatalf("no ExitError with status 1: %#v", err)
	}
	if got := Reason(err); got != "web did not come back from the backup" {
		t.Fatalf("Reason = %q", got)
	}
}

// What is not an answer stays an error and is not handed back as one.
func TestOnlyAJSONDocumentIsAnAnswer(t *testing.T) {
	srv := streamsServer(t)

	_, err := Run(srv, "backup", "drill", "text")
	if _, ok := Answered(err); ok {
		t.Fatal("text before a failing exit was taken for an answer")
	}

	_, err = Run(srv, "missing")
	if _, ok := Answered(err); ok {
		t.Fatal("a missing binary was taken for an answer")
	}
	if !strings.Contains(err.Error(), "command not found") || !strings.Contains(err.Error(), "homebutler deploy pi") {
		t.Fatalf("a missing binary lost its explanation: %v", err)
	}

	if _, ok := Answered(errors.New("dial tcp: connection refused")); ok {
		t.Fatal("a connection failure was taken for an answer")
	}
	if Reason(errors.New("dial tcp: connection refused")) != "" {
		t.Fatal("a connection failure has a remote reason")
	}
}
