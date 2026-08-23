package mcp

import (
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/watch"
)

// watch_check writes state and saves incidents, so it must not be advertised
// with the same risk as a query like system_status. A client that gates write
// tools behind confirmation relies on this classification.
func TestWatchCheckCapabilityIsWriteAndRemoteCapable(t *testing.T) {
	var found bool
	for _, c := range capabilityRegistry {
		if c.tool.Name != "watch_check" {
			continue
		}
		found = true
		if c.risk != riskWrite {
			t.Errorf("watch_check risk = %q, want %q", c.risk, riskWrite)
		}
		if !c.supports(targetServer) {
			t.Error("watch_check should be routable to a remote server")
		}
		if _, ok := c.tool.InputSchema.Properties["server"]; !ok {
			t.Error("watch_check should accept a server argument")
		}
		if len(c.tool.InputSchema.Required) != 0 {
			t.Errorf("watch_check should have no required arguments, got %v", c.tool.InputSchema.Required)
		}
	}
	if !found {
		t.Fatal("watch_check is not registered")
	}
}

func TestResolveIncidentCap(t *testing.T) {
	const defaultCap = 200

	tests := []struct {
		name string
		cfg  *config.Config
		want int
	}{
		{
			name: "unset takes the default rather than reading as unlimited",
			cfg:  &config.Config{},
			want: defaultCap,
		},
		{
			name: "config.yaml value wins",
			cfg: &config.Config{
				Watch: config.WatchRuntimeConfig{Retention: watch.RetentionConfig{MaxIncidents: 7}},
			},
			want: 7,
		},
		{
			name: "negative is the explicit way to ask for unlimited",
			cfg: &config.Config{
				Watch: config.WatchRuntimeConfig{Retention: watch.RetentionConfig{MaxIncidents: -1}},
			},
			want: 0,
		},
		{
			name: "nil config still resolves to the default",
			cfg:  nil,
			want: defaultCap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer(tt.cfg, "test")
			if got := s.resolveIncidentCap(t.TempDir()); got != tt.want {
				t.Errorf("resolveIncidentCap() = %d, want %d", got, tt.want)
			}
		})
	}
}

// The demo response has to show a skipped target. An agent that only ever saw
// an empty incident list would learn that it means the watch list is healthy,
// which is the reading Skipped exists to prevent.
func TestDemoWatchCheckReportsSkippedTargets(t *testing.T) {
	s := NewServer(&config.Config{}, "dev", true)

	res, err := s.executeDemoTool("watch_check", nil)
	if err != nil {
		t.Fatalf("watch_check demo failed: %v", err)
	}

	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("watch_check demo returned %T, want map", res)
	}

	skipped, ok := out["skipped"].([]map[string]any)
	if !ok || len(skipped) == 0 {
		t.Fatalf("demo response must carry a skipped target, got %v", out["skipped"])
	}
	if skipped[0]["kind"] == "docker" {
		t.Error("a docker target is never skipped by check; the demo should skip a systemd or pm2 target")
	}
	if _, ok := out["checked"]; !ok {
		t.Error("demo response is missing the checked count")
	}
}
