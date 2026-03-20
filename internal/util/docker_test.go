package util

import (
	"os"
	"testing"
)

func TestEnsureDockerHost_DoesNotPanic(t *testing.T) {
	// Should not panic even if docker is not available
	EnsureDockerHost()
}

func TestEnsureDockerHost_RespectsEnv(t *testing.T) {
	// Set a custom DOCKER_HOST
	orig := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "unix:///tmp/test.sock")
	defer os.Setenv("DOCKER_HOST", orig)

	EnsureDockerHost()

	// Should keep the custom value
	if os.Getenv("DOCKER_HOST") != "unix:///tmp/test.sock" {
		t.Error("EnsureDockerHost should respect existing DOCKER_HOST")
	}
}

func TestDockerCmd_DoesNotPanic(t *testing.T) {
	// Should not panic even with invalid command
	_, _ = DockerCmd("--version")
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{1000, "1000"},
		{999999, "999999"},
	}
	for _, tt := range tests {
		result := itoa(tt.input)
		if result != tt.expected {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
