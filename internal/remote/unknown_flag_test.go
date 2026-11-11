package remote

import (
	"errors"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// docker is a cobra program too, and its refusal quoted inside homebutler's
// own error is docker's flag, not one this homebutler lacks.
func TestAFlagSomeOtherProgramRefusedIsNotAVersionGap(t *testing.T) {
	err := remoteCommandError(&config.ServerConfig{Name: "pi"}, "1.5.0", []string{"docker", "restart", "web"},
		[]byte("error: failed to restart web: unknown flag: --bogus\nSee 'docker restart --help'."), errors.New("Process exited with status 1"))

	if strings.Contains(err.Error(), "does not have") || strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("docker's refusal was reported as an old homebutler: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to restart web") {
		t.Fatalf("the remote's own output is gone: %v", err)
	}
}
