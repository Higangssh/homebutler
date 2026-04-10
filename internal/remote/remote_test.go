package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"runtime"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// --- normalizeArch tests ---

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"amd64", "amd64"},
		{"i386", "i386"},
		{"riscv64", "riscv64"},
	}
	for _, tc := range tests {
		got := normalizeArch(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeArch(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// --- ValidateLocalArch tests ---

func TestValidateLocalArch_Match(t *testing.T) {
	err := ValidateLocalArch(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Errorf("expected nil for matching arch, got: %v", err)
	}
}

func TestValidateLocalArch_Mismatch(t *testing.T) {
	err := ValidateLocalArch("linux", "riscv64")
	if err == nil {
		t.Error("expected error for mismatched arch")
	}
	// Should contain cross-compile instructions
	if err != nil {
		msg := err.Error()
		if !contains(msg, "Cross-compile") {
			t.Errorf("error should contain cross-compile instructions, got: %s", msg)
		}
		if !contains(msg, "linux") || !contains(msg, "riscv64") {
			t.Errorf("error should mention target OS/arch, got: %s", msg)
		}
	}
}

// --- extractBinaryFromTarGz tests ---

func TestExtractBinaryFromTarGz_Valid(t *testing.T) {
	content := []byte("#!/bin/fake-homebutler")
	data := createTarGz(t, "homebutler", content)

	got, err := extractBinaryFromTarGz(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content mismatch: got %q, want %q", got, content)
	}
}

func TestExtractBinaryFromTarGz_InSubdir(t *testing.T) {
	content := []byte("#!/bin/fake-homebutler-v2")
	data := createTarGz(t, "homebutler_linux_arm64/homebutler", content)

	got, err := extractBinaryFromTarGz(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content mismatch: got %q, want %q", got, content)
	}
}

func TestExtractBinaryFromTarGz_NotFound(t *testing.T) {
	data := createTarGz(t, "README.md", []byte("# hello"))

	_, err := extractBinaryFromTarGz(data)
	if err == nil {
		t.Error("expected error when binary not found in archive")
	}
}

func TestExtractBinaryFromTarGz_InvalidData(t *testing.T) {
	_, err := extractBinaryFromTarGz([]byte("not a tar.gz"))
	if err == nil {
		t.Error("expected error for invalid tar.gz data")
	}
}

// --- ServerConfig helper tests ---

func TestServerConfigDefaults(t *testing.T) {
	s := &config.ServerConfig{Name: "test", Host: "1.2.3.4"}

	if s.SSHPort() != 22 {
		t.Errorf("expected default port 22, got %d", s.SSHPort())
	}
	if s.SSHUser() != "root" {
		t.Errorf("expected default user root, got %s", s.SSHUser())
	}
	if !s.UseKeyAuth() {
		t.Error("expected default auth to be key-based")
	}
	if s.SSHBinPath() != "homebutler" {
		t.Errorf("expected default bin path homebutler, got %s", s.SSHBinPath())
	}
}

func TestServerConfigCustom(t *testing.T) {
	s := &config.ServerConfig{
		Name:     "custom",
		Host:     "10.0.0.1",
		User:     "deploy",
		Port:     2222,
		AuthMode: "password",
		Password: "secret",
		BinPath:  "/opt/homebutler",
	}

	if s.SSHPort() != 2222 {
		t.Errorf("expected port 2222, got %d", s.SSHPort())
	}
	if s.SSHUser() != "deploy" {
		t.Errorf("expected user deploy, got %s", s.SSHUser())
	}
	if s.UseKeyAuth() {
		t.Error("expected password auth")
	}
	if s.SSHBinPath() != "/opt/homebutler" {
		t.Errorf("expected bin path /opt/homebutler, got %s", s.SSHBinPath())
	}
}

func TestServerConfigPasswordAuthNoPassword(t *testing.T) {
	s := &config.ServerConfig{
		Name:     "nopass",
		Host:     "1.2.3.4",
		AuthMode: "password",
		// Password intentionally empty
	}

	if s.UseKeyAuth() {
		t.Error("expected password auth mode")
	}
}

// --- Config FindServer tests ---

func TestFindServer(t *testing.T) {
	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "alpha", Host: "10.0.0.1", Local: true},
			{Name: "beta", Host: "10.0.0.2"},
			{Name: "gamma", Host: "10.0.0.3"},
		},
	}

	s := cfg.FindServer("beta")
	if s == nil {
		t.Fatal("expected to find server beta")
	} else if s.Host != "10.0.0.2" {
		t.Errorf("expected host 10.0.0.2, got %s", s.Host)
	}

	s = cfg.FindServer("alpha")
	if s == nil || !s.Local {
		t.Error("expected to find local server alpha")
	}

	s = cfg.FindServer("nonexistent")
	if s != nil {
		t.Error("expected nil for nonexistent server")
	}
}

// --- DeployResult tests ---

func TestDeployResult_Fields(t *testing.T) {
	r := DeployResult{
		Server:  "rpi5",
		Arch:    "linux/arm64",
		Source:  "local",
		Status:  "ok",
		Message: "installed",
	}
	if r.Server != "rpi5" {
		t.Errorf("unexpected server: %s", r.Server)
	}
	if r.Source != "local" {
		t.Errorf("unexpected source: %s", r.Source)
	}
}

func TestDeploy_ConnectFailure(t *testing.T) {
	server := &config.ServerConfig{
		Name:     "bad",
		Host:     "127.0.0.1",
		Port:     6553,
		AuthMode: "password",
		Password: "secret",
	}

	_, err := Deploy(server, "")
	if err == nil {
		t.Fatal("expected deploy connect failure")
	}
	if !contains(err.Error(), "ssh connect") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoteUpgrade_ConnectFailure(t *testing.T) {
	server := &config.ServerConfig{
		Name:     "bad",
		Host:     "127.0.0.1",
		Port:     6553,
		AuthMode: "password",
		Password: "secret",
	}

	result := RemoteUpgrade(server, "9.9.9")
	if result.Status != "error" {
		t.Fatalf("expected error status, got %s", result.Status)
	}
	if !contains(result.Message, "ssh connect") {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

// --- filterFlags (tested via export) ---
// filterFlags is in cmd package, so we test the logic indirectly

// --- helpers ---

func createTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: name,
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}

	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}
