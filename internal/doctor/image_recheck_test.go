package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/registry"
)

// behindSource is one running container that is behind its tag, asked
// through the real Checker against a registry that counts its HEADs. Every
// run builds a new Checker on the same cache file, as doctor runs do.
func behindSource(t *testing.T, cache string, heads *atomic.Int32) *ImageUpdateSource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heads.Add(1)
		w.Header().Set("Docker-Content-Digest", upstream)
		w.Header().Set("Content-Type", registry.MediaOCIIndex)
	}))
	t.Cleanup(srv.Close)
	return &ImageUpdateSource{
		ContainerImages: func([]string) (map[string]docker.ContainerImage, error) {
			return map[string]docker.ContainerImage{"c1": {ImageID: older, Reference: "nginx:1.27"}}, nil
		},
		ImageOrigins: func([]string) (map[string]docker.ImageOrigin, error) {
			return map[string]docker.ImageOrigin{older: {
				RepoDigests: []string{"nginx@" + older}, Known: true,
				PulledFrom: []string{"docker.io/library/nginx"}, MediaType: registry.MediaOCIIndex,
			}}, nil
		},
		ImageStore: func() (string, error) { return docker.StoreContainerd, nil },
		NewLookup: func(cfg LookupConfig) RegistryLookup {
			return &registry.Checker{
				Client:    &registry.Client{Endpoint: func(string) string { return srv.URL }},
				CachePath: cache, TTL: cfg.TTL, Now: cfg.Now, ReadOnly: cfg.NoSave,
			}
		},
	}
}

func runBehind(t *testing.T, src *ImageUpdateSource, now time.Time, noSave bool) {
	t.Helper()
	r := &Result{Findings: []Finding{}}
	inv := &inventory.Inventory{Containers: web("nginx:1.27")}
	checkImageUpdates(r, nil, inv, src, Options{Now: now, NoSave: noSave})
	if imageFinding(t, r, "web: nginx:1.27 now names a different image") == nil {
		t.Fatalf("run at %s: the behind container was not reported: %+v", now, r.Findings)
	}
}

// A container that is behind stays behind from one run to the next. Once a
// fresh answer has disagreed with the image it runs, the cached answer is
// enough until it expires; asking again every run would spend the Docker Hub
// limit on exactly the containers worth reporting.
func TestABehindContainerIsAskedAboutOncePerTTL(t *testing.T) {
	var heads atomic.Int32
	cache := filepath.Join(t.TempDir(), "digests.json")
	src := behindSource(t, cache, &heads)
	for i := 0; i < 20; i++ {
		runBehind(t, src, fixedNow.Add(time.Duration(i)*time.Minute), false)
	}
	if n := heads.Load(); n > 2 {
		t.Fatalf("%d HEADs over 20 runs inside the TTL, want 1 (2 at most)", n)
	}

	// Past the TTL it is asked again.
	before := heads.Load()
	runBehind(t, src, fixedNow.Add(7*time.Hour), false)
	if heads.Load() == before {
		t.Fatal("not asked again past the TTL")
	}
}

// Read mode writes nothing, so it cannot record what it confirmed: with no
// cache to read it asks every run, which the docs say. With a cache a saving
// run left behind, it reads that and asks nothing.
func TestReadModeAsksEachRunWithoutACacheAndNotWithOne(t *testing.T) {
	var heads atomic.Int32
	cache := filepath.Join(t.TempDir(), "digests.json")
	src := behindSource(t, cache, &heads)
	for i := 0; i < 5; i++ {
		runBehind(t, src, fixedNow.Add(time.Duration(i)*time.Minute), true)
	}
	if n := heads.Load(); n != 5 {
		t.Fatalf("%d HEADs over 5 read-only runs with no cache, want 5", n)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("read mode wrote the cache: %v", err)
	}

	runBehind(t, src, fixedNow.Add(10*time.Minute), false)
	before := heads.Load()
	for i := 0; i < 5; i++ {
		runBehind(t, src, fixedNow.Add(time.Duration(11+i)*time.Minute), true)
	}
	if heads.Load() != before {
		t.Fatalf("read mode asked %d more times with a confirmed answer cached", heads.Load()-before)
	}
}

// A cached answer from seconds ago was not asked for just now; on the Pi
// three runs twenty seconds apart all said "just now" from the same cache.
func TestCheckedAgoSaysJustNowOnlyForThisRun(t *testing.T) {
	if got := checkedAgo(fixedNow, fixedNow); got != "Asked the registry just now." {
		t.Fatalf("fresh = %q", got)
	}
	if got := checkedAgo(fixedNow.Add(-20*time.Second), fixedNow); got != "Registry answer from under a minute ago." {
		t.Fatalf("20s cached = %q", got)
	}
}

// The recheck is still made when the image running is a different one than
// the answer was confirmed against: that is the operator having pulled.
func TestARecheckStillFollowsAPull(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + upstream}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: older, Cached: true, CheckedAt: fixedNow.Add(-time.Hour), Confirmed: []string{platform}},
		},
		freshAnswer: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("nginx"), f)
	if len(f.lookup.rechecked) != 1 || imageFinding(t, r, "match what") == nil {
		t.Fatalf("rechecked %v, findings %+v", f.lookup.rechecked, r.Findings)
	}
}

// Docker's containerd store says an image was pulled as an index. Its
// digest is then an index digest, and one that is not the tag's is behind:
// there is no child to look for, so the index is not fetched.
func TestAnImagePulledByIndexDoesNotFetchTheIndex(t *testing.T) {
	f := &fakeImages{
		imageIDs: map[string]string{"c1": older},
		origins: map[string]docker.ImageOrigin{older: {
			RepoDigests: []string{"alpine@" + older}, Known: true,
			PulledFrom: []string{"docker.io/library/alpine"}, MediaType: registry.MediaDockerList,
		}},
		answers: map[string]registry.Result{
			"docker.io/library/alpine:3.20": {Outcome: registry.OutcomeDigest, Digest: upstream, MediaType: registry.MediaOCIIndex, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("alpine:3.20"), f)
	if len(f.lookup.indexed) != 0 {
		t.Fatalf("fetched %v for an image pulled by its index", f.lookup.indexed)
	}
	if imageFinding(t, r, "web: alpine:3.20 now names") == nil {
		t.Fatalf("findings = %+v", r.Findings)
	}

	// Pulled by a platform manifest's digest, it is fetched.
	f.origins[older] = docker.ImageOrigin{RepoDigests: []string{"alpine@" + older}, Known: true,
		PulledFrom: []string{"docker.io/library/alpine"}, MediaType: "application/vnd.oci.image.manifest.v1+json"}
	runImageCheck(t, nil, web("alpine:3.20"), f)
	if len(f.lookup.indexed) != 1 {
		t.Fatalf("indexed %v, want the index fetched once for a platform pull", f.lookup.indexed)
	}
	_ = context.Background
}
