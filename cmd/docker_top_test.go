package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
)

func captureOutput(t *testing.T, data any, jsonOut bool) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	outErr := output(data, jsonOut)
	os.Stdout = saved
	_ = w.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if outErr != nil {
		t.Fatalf("output: %v", outErr)
	}
	return string(got)
}

// The table on the operator's own terminal keeps the whole command line,
// which is what someone runs top to see; --json, which goes to scripts and
// agents, keeps only the process's name.
func TestDockerTopTextKeepsCommandLineJSONDoesNot(t *testing.T) {
	path := func(name string) string {
		abs, err := filepath.Abs(filepath.Join("..", "internal", "docker", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return abs
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = -o ]; then cat '" + path("top-secret-args-comm.txt") + "'; exit 0; fi\ndone\ncat '" + path("top-secret-args.txt") + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := docker.Top("probe")
	if err != nil {
		t.Fatalf("Top: %v", err)
	}

	text := captureOutput(t, result, false)
	for _, want := range []string{
		"/bin/sh -c sleep 3600 probe --token=FAKE123 hunter2-positional",
		"/usr/local/bin/probe serve --token=FAKE123 hunter2-positional",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text output lost the command line %q:\n%s", want, text)
		}
	}

	js := captureOutput(t, result, true)
	for _, secret := range []string{"FAKE123", "hunter2-positional", "--token"} {
		if strings.Contains(js, secret) {
			t.Errorf("--json output carries %q:\n%s", secret, js)
		}
	}
}
