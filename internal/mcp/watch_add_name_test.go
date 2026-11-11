package mcp

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/watch"
)

// watch_add without a server stored any container and unit it was given,
// while the same call naming a server met `watch add`'s rule there. A unit of
// --help reached systemctl as an option, `systemctl restart --help` exits 0,
// and the watcher counted it as a restart. Both routes now refuse the same
// names, before anything is written or sent.
func TestWatchAddRefusesTheSameNamesHereAndOnAServer(t *testing.T) {
	bad := []map[string]any{
		{"container": "-web"},
		{"container": "--help", "kind": "systemd"},
		{"container": "web", "kind": "systemd", "unit": "--help"},
		{"container": "web", "kind": "systemd", "unit": "nginx service; rm"},
		{"container": "a/b"},
	}

	t.Setenv("HOME", t.TempDir())
	s, _ := newTestServer()
	for _, args := range bad {
		if _, err := s.executeTool("watch_add", args); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("locally, watch_add %v = %v, want it refused", args, err)
		}
	}
	dir, err := watch.WatchDir()
	if err != nil {
		t.Fatal(err)
	}
	if targets, _ := watch.LoadTargets(dir); len(targets) != 0 {
		t.Fatalf("a refused target was stored: %+v", targets)
	}

	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	sent := false
	runRemote = func(_ *config.ServerConfig, _ ...string) ([]byte, error) {
		sent = true
		return []byte(`{"container":"web","kind":"docker","added":true}`), nil
	}
	for _, args := range bad {
		call := map[string]any{"server": "pi"}
		for k, v := range args {
			call[k] = v
		}
		if _, err := remoteServer(t).executeTool("watch_add", call); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("for a server, watch_add %v = %v, want it refused", args, err)
		}
	}
	if sent {
		t.Fatal("a refused name was sent to the server")
	}

	// A name the rule takes still goes through on both routes.
	if _, err := s.executeTool("watch_add", map[string]any{"container": "web", "kind": "pm2", "unit": "my-api"}); err != nil {
		t.Fatalf("a valid target was refused locally: %v", err)
	}
	if _, err := remoteServer(t).executeTool("watch_add", map[string]any{"server": "pi", "container": "web", "unit": "web.1"}); err != nil {
		t.Fatalf("a valid target was refused for a server: %v", err)
	}
}
