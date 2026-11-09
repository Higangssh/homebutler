//go:build windows

package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// key: ~\.ssh\id_ed25519 passed config validate on Windows and then failed to
// open when connecting, because only ~/ was expanded (#384).
func TestLoadKeyExpandsBackslashHomeOnWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)

	if got, want := expandKeyHome(`~\.ssh\id_ed25519`), filepath.Join(home, ".ssh", "id_ed25519"); got != want {
		t.Errorf("expandKeyHome = %q, want %q", got, want)
	}

	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte(testPrivateKeyPEM(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := loadKey(`~\.ssh\id_ed25519`)
	if err != nil {
		t.Fatalf(`loadKey(~\.ssh\id_ed25519) = %v`, err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("key type = %s", signer.PublicKey().Type())
	}
}

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}
