package mcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/registry"
	"github.com/Higangssh/homebutler/internal/system"
)

// Read mode promises nothing on disk changes, and the registry answer cache
// is the one file doctor writes. The cache is still read in read mode; it is
// written only by a server that may change things.
func TestReadModeDoctorDoesNotWriteTheRegistryCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	}))
	defer srv.Close()

	for _, tc := range []struct {
		risk  capability.Risk
		wrote bool
	}{
		{capability.RiskRead, false},
		{capability.RiskDestructive, true},
	} {
		t.Run(string(tc.risk), func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "registry", "digests.json")
			s, out := newTestServer()
			if err := s.SetMaxRisk(tc.risk); err != nil {
				t.Fatal(err)
			}
			s.doctorFns = func() doctor.CollectFuncs {
				return doctor.CollectFuncs{
					InventoryFns: inventory.CollectFuncs{
						StatusFn: func() (*system.StatusInfo, error) { return &system.StatusInfo{}, nil },
						DockerListFn: func() ([]docker.Container, error) {
							return []docker.Container{{ID: "c1", Name: "web", Image: "nginx", State: "running"}}, nil
						},
						PortsListFn: func() (*ports.Result, error) { return &ports.Result{}, nil },
					},
					SnapshotDir: t.TempDir(),
					WatchDir:    t.TempDir(),
					UpdatesFn:   func() doctor.UpdateState { return doctor.UpdateState{} },
					ImageUpdates: &doctor.ImageUpdateSource{
						ContainerImages: func([]string) (map[string]docker.ContainerImage, error) {
							return map[string]docker.ContainerImage{"c1": {ImageID: "sha256:cc", Reference: "nginx"}}, nil
						},
						ImageOrigins: func([]string) (map[string]docker.ImageOrigin, error) {
							return map[string]docker.ImageOrigin{"sha256:cc": {RepoDigests: []string{"nginx@sha256:bb"}}}, nil
						},
						ImageStore: func() (string, error) { return docker.StoreClassic, nil },
						NewLookup: func(cfg doctor.LookupConfig) doctor.RegistryLookup {
							return &registry.Checker{
								Client:    &registry.Client{Endpoint: func(string) string { return srv.URL }},
								CachePath: cache, TTL: cfg.TTL, Now: cfg.Now, ReadOnly: cfg.NoSave,
							}
						},
					},
				}
			}
			r := callOn(t, s, out, "doctor", map[string]any{})
			if r.IsError {
				t.Fatalf("doctor failed: %+v", r)
			}
			_, err := os.Stat(cache)
			if wrote := err == nil; wrote != tc.wrote {
				t.Fatalf("cache written = %v, want %v", wrote, tc.wrote)
			}
		})
	}
}

// The same promise holds for a server: read mode sends --no-save, so the
// doctor there does not write its cache either.
func TestReadModeDoctorOnAServerSendsNoSave(t *testing.T) {
	for _, tc := range []struct {
		risk capability.Risk
		want bool
	}{
		{capability.RiskRead, true},
		{capability.RiskDestructive, false},
	} {
		t.Run(string(tc.risk), func(t *testing.T) {
			var sent []string
			saved := runRemote
			t.Cleanup(func() { runRemote = saved })
			runRemote = func(_ *config.ServerConfig, argv ...string) ([]byte, error) {
				sent = argv
				return []byte(`{"schema_version":1,"status":"pass","summary":{"pass":0,"warn":0,"fail":0},"findings":[]}`), nil
			}
			s := remoteServer(t)
			if err := s.SetMaxRisk(tc.risk); err != nil {
				t.Fatal(err)
			}
			_, _ = s.executeTool("doctor", map[string]any{"server": "pi"})
			if got := slices.Contains(sent, "--no-save"); got != tc.want {
				t.Fatalf("sent %v; --no-save = %v, want %v", sent, got, tc.want)
			}
		})
	}
}

// A server older than --no-save is older than the image check and its
// cache, so asking it again without the flag is still read-only. The retry
// is for that refusal only, and only once.
func TestReadModeDoctorRetriesAnOlderServerWithoutNoSave(t *testing.T) {
	for name, refusal := range map[string]string{
		"version named": "[pi] the homebutler on pi is 1.5.0, and its `doctor` does not have --no-save",
		"cobra's line":  "[pi] remote command failed: exit status 1\n  → Output: error: unknown flag: --no-save",
	} {
		t.Run(name, func(t *testing.T) {
			var calls [][]string
			saved := runRemote
			t.Cleanup(func() { runRemote = saved })
			runRemote = func(_ *config.ServerConfig, argv ...string) ([]byte, error) {
				calls = append(calls, argv)
				if slices.Contains(argv, "--no-save") {
					return nil, errors.New(refusal)
				}
				return []byte(`{"schema_version":1,"status":"pass","summary":{"pass":1,"warn":0,"fail":0},"findings":[]}`), nil
			}
			s := remoteServer(t)
			if err := s.SetMaxRisk(capability.RiskRead); err != nil {
				t.Fatal(err)
			}
			if _, err := s.executeTool("doctor", map[string]any{"server": "pi"}); err != nil {
				t.Fatalf("doctor = %v, want the retry's answer", err)
			}
			if len(calls) != 2 || slices.Contains(calls[1], "--no-save") {
				t.Fatalf("calls = %v, want one with --no-save and one without", calls)
			}
		})
	}

	// Any other failure is not retried.
	var calls int
	saved := runRemote
	t.Cleanup(func() { runRemote = saved })
	runRemote = func(_ *config.ServerConfig, argv ...string) ([]byte, error) {
		calls++
		return nil, errors.New("[pi] SSH connection failed")
	}
	s := remoteServer(t)
	_ = s.SetMaxRisk(capability.RiskRead)
	if _, err := s.executeTool("doctor", map[string]any{"server": "pi"}); err == nil || calls != 1 {
		t.Fatalf("err %v after %d calls, want the failure after one", err, calls)
	}
}
