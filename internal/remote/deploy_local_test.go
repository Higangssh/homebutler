package remote

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// A --local path that does not exist used to be found only after connecting
// and choosing an install directory, which can create ~/.local/bin on the
// server. It is now refused before the server is contacted at all.
func TestDeployReadsTheLocalBinaryBeforeConnecting(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "homebutler")
	// Nothing listens on port 1: reaching connect would fail with an SSH
	// error instead of the read error.
	server := &config.ServerConfig{Name: "rpi5", Host: "127.0.0.1", Port: 1, AuthMode: "password", Password: "x"}

	_, err := Deploy(server, missing, "")
	if err == nil {
		t.Fatal("deploy with a missing local binary succeeded")
	}
	if !strings.HasPrefix(err.Error(), "read local binary: ") {
		t.Errorf("want the local read to fail first, got %v", err)
	}
}
