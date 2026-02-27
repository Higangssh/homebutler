package util

import (
	"runtime"
	"testing"
)

func TestRunCmd(t *testing.T) {
	out, err := RunCmd("echo", "hello")
	if err != nil {
		t.Fatalf("RunCmd echo failed: %v", err)
	}
	if out != "hello" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunCmdError(t *testing.T) {
	cmd := "false"
	if runtime.GOOS == "windows" {
		cmd = "cmd"
	}
	_, err := RunCmd(cmd)
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}
