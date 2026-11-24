package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/registry"
)

const (
	upstream = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	older    = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// configID is an image ID: the digest of the image's config, which is
	// never any registry manifest's digest.
	configID = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	platform = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

// fakeLookup answers Check from answers, Recheck from fresh, and Index from
// index, and records what it was asked.
type fakeLookup struct {
	cfg       LookupConfig
	answers   map[string]registry.Result
	fresh     map[string]registry.Result
	index     map[string]registry.Result
	saveErr   error
	asked     []string
	rechecked []string
	indexed   []string
	confirmed []string
}

func (l *fakeLookup) Check(_ context.Context, refs []registry.Ref) map[string]registry.Result {
	out := map[string]registry.Result{}
	for _, r := range refs {
		l.asked = append(l.asked, r.String())
		if a, ok := l.answers[r.String()]; ok {
			out[r.String()] = a
		}
	}
	return out
}

func (l *fakeLookup) Recheck(_ context.Context, refs []registry.Ref) map[string]registry.Result {
	out := map[string]registry.Result{}
	for _, r := range refs {
		l.rechecked = append(l.rechecked, r.String())
		if a, ok := l.fresh[r.String()]; ok {
			out[r.String()] = a
		}
	}
	return out
}

func (l *fakeLookup) Index(_ context.Context, ref registry.Ref, digest string) registry.Result {
	key := ref.Name() + "@" + digest
	l.indexed = append(l.indexed, key)
	if a, ok := l.index[key]; ok {
		return a
	}
	return registry.Result{Outcome: registry.OutcomeUnreachable, Reason: "not in the fake"}
}

func (l *fakeLookup) SaveError() error { return l.saveErr }

func (l *fakeLookup) Confirm(ref registry.Ref, running []string) {
	l.confirmed = append(l.confirmed, ref.String())
}

type fakeImages struct {
	imageIDs map[string]string
	// references is the Config.Image docker inspect gives; a container
	// without one falls back to what docker ps printed.
	references map[string]string
	// repoDigests answers with no Identity, as the classic store and Docker
	// before 29.2 do.
	repoDigests map[string][]string
	// origins overrides repoDigests for an image.
	origins map[string]docker.ImageOrigin
	// store is what docker info says; empty means classic.
	store       string
	storeAsked  int
	lookup      fakeLookup
	answers     map[string]registry.Result
	freshAnswer map[string]registry.Result
}

func (f *fakeImages) source() *ImageUpdateSource {
	if f.store == "" {
		f.store = docker.StoreClassic
	}
	f.lookup.answers = f.answers
	f.lookup.fresh = f.freshAnswer
	return &ImageUpdateSource{
		ContainerImages: func(ids []string) (map[string]docker.ContainerImage, error) {
			out := map[string]docker.ContainerImage{}
			for id, image := range f.imageIDs {
				out[id] = docker.ContainerImage{ImageID: image, Reference: f.references[id]}
			}
			return out, nil
		},
		ImageOrigins: func(ids []string) (map[string]docker.ImageOrigin, error) {
			out := map[string]docker.ImageOrigin{}
			for id, digests := range f.repoDigests {
				out[id] = docker.ImageOrigin{RepoDigests: digests}
			}
			for id, o := range f.origins {
				out[id] = o
			}
			return out, nil
		},
		ImageStore: func() (string, error) {
			f.storeAsked++
			if f.store == "unknown" {
				return "", errors.New("docker info failed")
			}
			return f.store, nil
		},
		NewLookup: func(cfg LookupConfig) RegistryLookup {
			f.lookup.cfg = cfg
			return &f.lookup
		},
	}
}

func imageFinding(t *testing.T, r *Result, titlePart string) *Finding {
	t.Helper()
	for i := range r.Findings {
		if r.Findings[i].Category == "docker" && strings.Contains(r.Findings[i].Title, titlePart) {
			return &r.Findings[i]
		}
	}
	return nil
}

func runImageCheck(t *testing.T, cfg *config.Config, containers []docker.Container, f *fakeImages) *Result {
	t.Helper()
	r := &Result{Findings: []Finding{}}
	checkImageUpdates(r, cfg, &inventory.Inventory{Containers: containers}, f.source(), Options{Now: fixedNow})
	return r
}

func web(image string) []docker.Container {
	return []docker.Container{{ID: "c1", Name: "web", Image: image, State: "running"}}
}

func TestImageUpdateNamesAContainerWhoseTagMoved(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + older}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:1.27": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow.Add(-2 * time.Hour)},
		},
	}
	r := runImageCheck(t, nil, web("nginx:1.27"), f)

	got := imageFinding(t, r, "web: nginx:1.27 now names a different image in the registry")
	if got == nil {
		t.Fatalf("no finding: %+v", r.Findings)
	}
	if got.Severity != SeverityInfo {
		t.Fatalf("severity = %s; a tag that moved is not a fault, and a warn would turn --strict red", got.Severity)
	}
	if strings.Contains(got.Title, "newer") {
		t.Fatalf("title = %q; a tag moved back to an older image is not newer", got.Title)
	}
	if !strings.Contains(got.Detail, "sha256:aaaaaaaaaaaa") || !strings.Contains(got.Detail, "sha256:bbbbbbbbbbbb") || !strings.Contains(got.Detail, "2h ago") {
		t.Fatalf("detail = %q, want both digests and the age of the answer", got.Detail)
	}
	if got.Command != "" {
		t.Fatalf("command = %q; this check suggests nothing to run", got.Command)
	}
	s := summarize(r.Findings)
	if s.Info != 1 || s.Warn != 0 || overallStatus(s) != SeverityPass {
		t.Fatalf("summary = %+v, status %s; info is counted and never sets the status", s, overallStatus(s))
	}
}

// The image ID equalling the registry digest proves nothing — it is the
// config digest — and a RepoDigests entry equalling it proves the image is
// current. Comparing the wrong one either way gets every image wrong.
func TestImageUpdateComparesRepoDigestsNotTheImageID(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + upstream}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("nginx"), f)
	if imageFinding(t, r, "different image") != nil {
		t.Fatalf("an image whose RepoDigests holds the registry digest was called changed: %+v", r.Findings)
	}
	got := imageFinding(t, r, "match what their registry serves")
	if got == nil || !strings.Contains(got.Detail, "web") {
		t.Fatalf("no up-to-date finding naming web: %+v", r.Findings)
	}
	if got.Severity != SeverityPass {
		t.Fatalf("a match is %s, want pass", got.Severity)
	}
}

// An image pulled again under the same name, or pushed twice, carries more
// than one digest for the repository; any of them matching is a match.
func TestImageUpdateTakesAnyRepoDigestAsAMatch(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + older, "nginx@" + upstream}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("nginx"), f)
	if imageFinding(t, r, "different image") != nil || imageFinding(t, r, "match what") == nil {
		t.Fatalf("the second RepoDigests entry was not taken as a match: %+v", r.Findings)
	}
}

// A digest recorded for another repository says nothing about this one.
func TestImageUpdateOnlyComparesDigestsFromTheSameRepository(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"ghcr.io/someone/nginx@" + upstream}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("nginx:latest"), f)
	got := imageFinding(t, r, "not checked")
	if got == nil || !strings.Contains(got.Detail, "pulled under another name") {
		t.Fatalf("want a skip naming the other repository: %+v", r.Findings)
	}
	if len(f.lookup.asked) != 0 {
		t.Fatalf("asked the registry about %v with nothing to compare its answer to", f.lookup.asked)
	}
}

func TestImageUpdateSaysWhatItSkippedAndWhy(t *testing.T) {
	f := &fakeImages{
		imageIDs: map[string]string{"c1": configID, "c2": "sha256:local", "c3": configID, "c4": configID, "c5": configID, "c6": configID, "c8": configID},
		// Under the containerd store docker ps prints the image ID for a
		// container created from a digest; Config.Image still says so.
		references: map[string]string{"c8": "alpine@" + upstream},
		repoDigests: map[string][]string{
			"sha256:local": {},
			configID:       {"nginx@" + older, "ghcr.io/acme/app@" + older, "registry.lan/app@" + older, "localhost:5000/x@" + older},
		},
		answers: map[string]registry.Result{
			"ghcr.io/acme/app:1":             {Outcome: registry.OutcomeAuth, Reason: "ghcr.io would not show acme/app without credentials"},
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeRateLimited, Reason: "docker.io is rate limiting this address"},
			"registry.lan/app:latest":        {Outcome: registry.OutcomeUnreachable, Reason: "no such host"},
		},
	}
	containers := []docker.Container{
		{ID: "c1", Name: "pinned", Image: "alpine@" + upstream, State: "running"},
		{ID: "c2", Name: "built", Image: "myapp:dev", State: "running"},
		{ID: "c3", Name: "private", Image: "ghcr.io/acme/app:1", State: "running"},
		{ID: "c4", Name: "limited", Image: "nginx", State: "running"},
		{ID: "c5", Name: "offline", Image: "registry.lan/app", State: "running"},
		{ID: "c6", Name: "moved", Image: "0123456789ab", State: "running"},
		{ID: "c7", Name: "stopped", Image: "nginx", State: "exited"},
		{ID: "c8", Name: "pinned-ps-id", Image: "77726ef6b57d", State: "running"},
	}
	r := runImageCheck(t, nil, containers, f)

	got := imageFinding(t, r, "not checked")
	if got == nil {
		t.Fatalf("no skip finding: %+v", r.Findings)
	}
	if got.Severity != SeverityInfo {
		t.Fatalf("severity = %s; an offline machine must not turn --strict red", got.Severity)
	}
	if !strings.HasPrefix(got.Title, "7 ") {
		t.Fatalf("title = %q, want seven running containers skipped", got.Title)
	}
	for _, want := range []string{
		"pinned (alpine@" + upstream + "): pinned by digest",
		"built (myapp:dev): built on this machine",
		"private (ghcr.io/acme/app:1): the registry would not answer without credentials",
		"limited (nginx): the registry is rate limiting",
		"offline (registry.lan/app): could not reach the registry: no such host",
		"moved (0123456789ab): created from an image ID",
		"pinned-ps-id (alpine@" + upstream + "): pinned by digest",
	} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail is missing %q:\n%s", want, got.Detail)
		}
	}
	if strings.Contains(got.Detail, "stopped") {
		t.Errorf("a stopped container was checked: %s", got.Detail)
	}
	for _, f := range r.Findings {
		if strings.Contains(f.Title, "different image") || strings.Contains(f.Title, "match what") {
			t.Errorf("a skipped image was counted as checked: %+v", f)
		}
	}
}

func TestImageUpdateCheckCanBeTurnedOff(t *testing.T) {
	off := false
	cfg := &config.Config{}
	cfg.Doctor.ImageUpdates.Enabled = &off
	f := &fakeImages{imageIDs: map[string]string{"c1": configID}, repoDigests: map[string][]string{configID: {"nginx@" + older}}}
	r := runImageCheck(t, cfg, web("nginx"), f)
	if len(r.Findings) != 0 || len(f.lookup.asked) != 0 {
		t.Fatalf("enabled: false still asked %v and found %+v", f.lookup.asked, r.Findings)
	}
}

func TestImageUpdateTakesTheConfiguredTTLAndNoSave(t *testing.T) {
	cfg := &config.Config{}
	cfg.Doctor.ImageUpdates.CacheTTL = "90m"
	f := &fakeImages{imageIDs: map[string]string{"c1": configID}, repoDigests: map[string][]string{configID: {"nginx@" + older}}}
	r := &Result{Findings: []Finding{}}
	checkImageUpdates(r, cfg, &inventory.Inventory{Containers: web("nginx")}, f.source(), Options{Now: fixedNow, NoSave: true})
	if f.lookup.cfg.TTL != 90*time.Minute || !f.lookup.cfg.NoSave || !f.lookup.cfg.Now.Equal(fixedNow) {
		t.Fatalf("lookup opened with %+v, want 90m, NoSave and the run's clock", f.lookup.cfg)
	}
}

func TestImageUpdateSurvivesDockerInspectFailing(t *testing.T) {
	src := &ImageUpdateSource{
		ContainerImages: func([]string) (map[string]docker.ContainerImage, error) {
			return map[string]docker.ContainerImage{"c1": {ImageID: configID}}, nil
		},
		ImageOrigins: func([]string) (map[string]docker.ImageOrigin, error) {
			return nil, errors.New("daemon went away")
		},
		NewLookup: func(LookupConfig) RegistryLookup {
			t.Fatal("opened the registries with nothing to compare against")
			return nil
		},
	}
	r := &Result{Findings: []Finding{}}
	checkImageUpdates(r, nil, &inventory.Inventory{Containers: web("nginx")}, src, Options{Now: fixedNow})
	if got := imageFinding(t, r, "not checked"); got == nil || !strings.Contains(got.Detail, "daemon went away") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// Tests that build CollectFuncs by hand must not reach the internet. Nil is
// "skip", not "take the real one" as it is for InspectFn.
func TestRunWithoutAnImageSourceAsksNoRegistry(t *testing.T) {
	fns := doctorFuncs(healthyStatus(), web("nginx"), nil, nil, []backup.ListEntry{})
	r, err := Run(&config.Config{}, fns, Options{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if strings.Contains(f.Title, "image") {
			t.Fatalf("image check ran without a source: %+v", f)
		}
	}
	if DefaultCollectFuncs().ImageUpdates == nil {
		t.Fatal("the real doctor has no image update source")
	}
}

// The containerd image store gives an image built here a RepoDigests entry
// under its own name. Taking that as a pull would send the name to Docker
// Hub and report its "no such image" as a registry wanting credentials.
func TestImageUpdateKnowsABuildUnderTheContainerdStore(t *testing.T) {
	f := &fakeImages{
		imageIDs: map[string]string{"c1": "sha256:d614", "c2": upstream},
		origins: map[string]docker.ImageOrigin{
			"sha256:d614": {RepoDigests: []string{"hb412-local@sha256:d614"}, Known: true, Built: true},
			// Pulled as alpine, then tagged as something else.
			upstream: {RepoDigests: []string{"alpine@" + upstream, "myalpine@" + upstream}, Known: true, PulledFrom: []string{"docker.io/library/alpine"}},
		},
		store: docker.StoreContainerd,
	}
	r := runImageCheck(t, nil, []docker.Container{
		{ID: "c1", Name: "built", Image: "hb412-local:dev", State: "running"},
		{ID: "c2", Name: "renamed", Image: "myalpine:1", State: "running"},
	}, f)
	if len(f.lookup.asked) != 0 {
		t.Fatalf("asked a registry about %v", f.lookup.asked)
	}
	got := imageFinding(t, r, "not checked")
	if got == nil || !strings.Contains(got.Detail, "built (hb412-local:dev): built on this machine") ||
		!strings.Contains(got.Detail, "renamed (myalpine:1): its image was pulled under another name") {
		t.Fatalf("findings = %+v", r.Findings)
	}
	if f.storeAsked != 0 {
		t.Fatalf("asked docker info %d times with Identity on every image", f.storeAsked)
	}
}

// Docker before 29.2, or a client pinned to API 1.52, reports no Identity.
// Under the containerd store a local build then looks pulled, so nothing is
// asked: not the build's name, and not the pulled image beside it either,
// since nothing tells the two apart.
func TestImageUpdateWithoutIdentityUnderTheContainerdStoreAsksNothing(t *testing.T) {
	for _, store := range []string{docker.StoreContainerd, "unknown"} {
		t.Run(store, func(t *testing.T) {
			f := &fakeImages{
				imageIDs:    map[string]string{"c1": "sha256:d614", "c2": older},
				repoDigests: map[string][]string{"sha256:d614": {"hb412-local@sha256:d614"}, older: {"alpine@" + older}},
				store:       store,
			}
			r := runImageCheck(t, nil, []docker.Container{
				{ID: "c1", Name: "built", Image: "hb412-local:dev", State: "running"},
				{ID: "c2", Name: "pulled", Image: "alpine:3.20", State: "running"},
			}, f)
			if len(f.lookup.asked) != 0 {
				t.Fatalf("sent %v to a registry", f.lookup.asked)
			}
			got := imageFinding(t, r, "2 running container(s) not checked")
			if got == nil || !strings.Contains(got.Detail, "registry was not asked") {
				t.Fatalf("findings = %+v", r.Findings)
			}
			if f.storeAsked != 1 {
				t.Fatalf("docker info asked %d times, want once", f.storeAsked)
			}
		})
	}
}

// Under the containerd store the image ID is the index digest, and it is in
// RepoDigests too: the same comparison holds.
func TestImageUpdateUnderTheContainerdStore(t *testing.T) {
	f := &fakeImages{
		imageIDs: map[string]string{"c1": older},
		origins: map[string]docker.ImageOrigin{
			older: {RepoDigests: []string{"alpine@" + older}, Known: true, PulledFrom: []string{"docker.io/library/alpine"}},
		},
		answers: map[string]registry.Result{
			"docker.io/library/alpine:3.20": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
		store: docker.StoreContainerd,
	}
	r := runImageCheck(t, nil, []docker.Container{{ID: "c1", Name: "old", Image: "alpine:3.20", State: "running"}}, f)
	if imageFinding(t, r, "old: alpine:3.20 now names a different image") == nil {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// docker ps prints an image ID when the tag a container was created from
// names another image here now. The reference is still the tag, so the
// registry is still asked, and the finding says the pull may already be done.
func TestImageUpdateAfterAPullWithoutARecreate(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		references:  map[string]string{"c1": "nginx:1.27"},
		repoDigests: map[string][]string{configID: {"nginx@" + older}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:1.27": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("cccccccccccc"), f)
	got := imageFinding(t, r, "web: nginx:1.27 now names")
	if got == nil || !strings.Contains(got.Detail, "not recreated") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// The operator pulled and recreated inside the TTL: the cache still holds
// the digest that was upstream before, which is now the one running... or
// the old one, which no longer is. A cached answer that disagrees with what
// runs is asked again, and only the fresh answer is used.
func TestImageUpdateAsksAgainBeforeReportingACachedDisagreement(t *testing.T) {
	f := &fakeImages{
		imageIDs: map[string]string{"c1": configID},
		// Running the update now.
		repoDigests: map[string][]string{configID: {"nginx@" + upstream}},
		answers: map[string]registry.Result{
			// Cached an hour ago, before upstream moved twice.
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: older, CheckedAt: fixedNow.Add(-time.Hour), Cached: true},
		},
		freshAnswer: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("nginx"), f)
	if len(f.lookup.rechecked) != 1 {
		t.Fatalf("rechecked %v, want the one cached disagreement", f.lookup.rechecked)
	}
	if imageFinding(t, r, "different image") != nil {
		t.Fatalf("reported a stale cached answer: %+v", r.Findings)
	}
	if imageFinding(t, r, "match what") == nil {
		t.Fatalf("the fresh answer was not used: %+v", r.Findings)
	}
}

// A fresh answer is not asked again, and a cached one that cannot be
// confirmed is not reported.
func TestImageUpdateReportsOnlyAFreshDisagreement(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID, "c2": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + older, "redis@" + older}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
			"docker.io/library/redis:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow.Add(-time.Hour), Cached: true},
		},
		freshAnswer: map[string]registry.Result{
			"docker.io/library/redis:latest": {Outcome: registry.OutcomeUnreachable, Reason: "no answer in time"},
		},
	}
	r := runImageCheck(t, nil, []docker.Container{
		{ID: "c1", Name: "web", Image: "nginx", State: "running"},
		{ID: "c2", Name: "cache", Image: "redis", State: "running"},
	}, f)
	if strings.Join(f.lookup.rechecked, ",") != "docker.io/library/redis:latest" {
		t.Fatalf("rechecked %v, want only the cached one", f.lookup.rechecked)
	}
	if imageFinding(t, r, "web: nginx now names") == nil {
		t.Fatalf("the fresh disagreement was not reported: %+v", r.Findings)
	}
	if imageFinding(t, r, "cache: redis now names") != nil {
		t.Fatalf("a cached disagreement was reported without being confirmed: %+v", r.Findings)
	}
	if got := imageFinding(t, r, "not checked"); got == nil || !strings.Contains(got.Detail, "cache (redis): the cached answer disagrees") {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

// Pulled by one platform's manifest digest and tagged: RepoDigests holds the
// platform digest, the tag answers with the index digest, and the two differ
// though nothing moved. The index's list is what settles it.
func TestImageUpdateMatchesAPlatformDigestInTheIndex(t *testing.T) {
	index := registry.Result{Outcome: registry.OutcomeDigest, Digest: upstream, MediaType: "application/vnd.oci.image.index.v1+json", CheckedAt: fixedNow}
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"alpine@" + platform}},
		answers:     map[string]registry.Result{"docker.io/library/alpine:3.20": index},
	}
	f.lookup.index = map[string]registry.Result{
		"docker.io/library/alpine@" + upstream: {Outcome: registry.OutcomeDigest, Digest: upstream, Children: []string{older, platform}},
	}
	r := runImageCheck(t, nil, web("alpine:3.20"), f)
	if imageFinding(t, r, "different image") != nil || imageFinding(t, r, "match what") == nil {
		t.Fatalf("a platform digest in the index was not a match: %+v", r.Findings)
	}

	// The same tag after it moved: the platform digest is in no child.
	f.lookup.index["docker.io/library/alpine@"+upstream] = registry.Result{Outcome: registry.OutcomeDigest, Digest: upstream, Children: []string{older}}
	f.lookup.indexed = nil
	r = runImageCheck(t, nil, web("alpine:3.20"), f)
	if imageFinding(t, r, "web: alpine:3.20 now names") == nil {
		t.Fatalf("a platform digest not in the index was not reported: %+v", r.Findings)
	}

	// An index that cannot be read leaves it unsaid.
	delete(f.lookup.index, "docker.io/library/alpine@"+upstream)
	r = runImageCheck(t, nil, web("alpine:3.20"), f)
	if imageFinding(t, r, "different image") != nil || imageFinding(t, r, "not checked") == nil {
		t.Fatalf("reported without reading the index: %+v", r.Findings)
	}
}

// A single-platform tag has no index to look in.
func TestImageUpdateDoesNotAskForTheIndexOfAManifest(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + older}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, MediaType: "application/vnd.oci.image.manifest.v1+json", CheckedAt: fixedNow},
		},
	}
	r := runImageCheck(t, nil, web("nginx"), f)
	if len(f.lookup.indexed) != 0 || imageFinding(t, r, "now names") == nil {
		t.Fatalf("indexed %v, findings %+v", f.lookup.indexed, r.Findings)
	}
}

func TestImageUpdateSaysOnceWhenTheCacheCannotBeSaved(t *testing.T) {
	f := &fakeImages{
		imageIDs:    map[string]string{"c1": configID},
		repoDigests: map[string][]string{configID: {"nginx@" + upstream}},
		answers: map[string]registry.Result{
			"docker.io/library/nginx:latest": {Outcome: registry.OutcomeDigest, Digest: upstream, CheckedAt: fixedNow},
		},
	}
	f.lookup.saveErr = errors.New("writing digests.json: permission denied")
	r := runImageCheck(t, nil, web("nginx"), f)
	n := 0
	for _, x := range r.Findings {
		if strings.Contains(x.Title, "could not be saved") {
			n++
			if x.Severity != SeverityInfo || !strings.Contains(x.Detail, "permission denied") {
				t.Fatalf("finding = %+v", x)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d save findings, want one: %+v", n, r.Findings)
	}
}

// The production wiring, not a copy of it: DefaultImageUpdateSource's
// lookup is a Checker writing ~/.homebutler/registry/digests.json for the
// configured TTL, and NoSave reaches it as ReadOnly.
func TestDefaultImageUpdateSourceWiring(t *testing.T) {
	src := DefaultImageUpdateSource()
	if src.ContainerImages == nil || src.ImageOrigins == nil || src.ImageStore == nil || src.NewLookup == nil {
		t.Fatalf("a real source is missing a reader: %+v", src)
	}
	for _, noSave := range []bool{false, true} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		l := src.NewLookup(LookupConfig{TTL: 90 * time.Minute, Now: fixedNow, NoSave: noSave})
		c, ok := l.(*registry.Checker)
		if !ok {
			t.Fatalf("lookup is %T, want *registry.Checker", l)
		}
		want := filepath.Join(home, ".homebutler", "registry", "digests.json")
		if c.CachePath != want || c.TTL != 90*time.Minute || !c.Now.Equal(fixedNow) || c.ReadOnly != noSave || c.Client == nil || c.Client.Endpoint != nil {
			t.Fatalf("NoSave %v: checker = %+v, want cache %s, 90m, the run's clock, ReadOnly %v and the real endpoints", noSave, c, want, noSave)
		}

		// Through the real Checker, with only the endpoint swapped.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Docker-Content-Digest", upstream)
		}))
		c.Client.Endpoint = func(string) string { return srv.URL }
		ref, _ := registry.ParseReference("nginx")
		c.Check(context.Background(), []registry.Ref{ref})
		srv.Close()
		_, err := os.Stat(want)
		if wrote := err == nil; wrote == noSave {
			t.Fatalf("NoSave %v: cache written = %v", noSave, wrote)
		}
	}
}

// End to end through the real Checker and Client against a fake registry,
// so the pieces agree on cache keys, on what a digest looks like, and on the
// index's children.
func TestImageUpdateThroughTheRegistryClient(t *testing.T) {
	indexBody := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":%q},{"digest":%q}]}`, older, platform)
	sum := sha256.Sum256([]byte(indexBody))
	indexDigest := "sha256:" + hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = fmt.Fprint(w, `{"access_token":"t"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="ghcr.io"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
		w.Header().Set("Docker-Content-Digest", indexDigest)
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, indexBody)
		}
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "digests.json")
	src := &ImageUpdateSource{
		ContainerImages: func([]string) (map[string]docker.ContainerImage, error) {
			return map[string]docker.ContainerImage{"c1": {ImageID: configID, Reference: "ghcr.io/acme/app:1"}, "c2": {ImageID: "sha256:other", Reference: "ghcr.io/acme/app:1"}}, nil
		},
		ImageOrigins: func([]string) (map[string]docker.ImageOrigin, error) {
			return map[string]docker.ImageOrigin{
				configID:       {RepoDigests: []string{"ghcr.io/acme/app@" + platform}},
				"sha256:other": {RepoDigests: []string{"ghcr.io/acme/app@" + upstream}},
			}, nil
		},
		ImageStore: func() (string, error) { return docker.StoreClassic, nil },
		NewLookup: func(cfg LookupConfig) RegistryLookup {
			return &registry.Checker{Client: &registry.Client{Endpoint: func(string) string { return srv.URL }}, CachePath: cache, TTL: cfg.TTL, Now: cfg.Now, ReadOnly: cfg.NoSave}
		},
	}
	r := &Result{Findings: []Finding{}}
	inv := &inventory.Inventory{Containers: []docker.Container{
		{ID: "c1", Name: "app", Image: "ghcr.io/acme/app:1", State: "running"},
		{ID: "c2", Name: "app2", Image: "ghcr.io/acme/app:1", State: "running"},
	}}
	checkImageUpdates(r, nil, inv, src, Options{Now: fixedNow, NoSave: true})
	if got := imageFinding(t, r, "match what"); got == nil || got.Detail != "app. Asked the registry just now." {
		t.Fatalf("the platform digest was not matched through the index: %+v", r.Findings)
	}
	if imageFinding(t, r, "app2: ghcr.io/acme/app:1 now names") == nil {
		t.Fatalf("a digest in neither the tag nor the index was not reported: %+v", r.Findings)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("NoSave wrote the cache: %v", err)
	}
	r = &Result{Findings: []Finding{}}
	checkImageUpdates(r, nil, inv, src, Options{Now: fixedNow})
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("the cache was not written: %v", err)
	}
}
