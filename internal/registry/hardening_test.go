package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// A registry served over https may not send the anonymous token request to
// a plain-http realm, where it can be read and answered by anyone on the
// path. A registry that is itself plain http, such as one on localhost:5000,
// may.
func TestTokenRealmMustBeHTTPSForAnHTTPSRegistry(t *testing.T) {
	var tokenAsked atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenAsked.Add(1)
		_, _ = fmt.Fprint(w, `{"token":"t"}`)
	}))
	defer plain.Close()

	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+plain.URL+`/token",service="x"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", indexDigest)
	}))
	defer tls.Close()

	c := &Client{HTTP: tls.Client(), Endpoint: func(string) string { return tls.URL }}
	res := c.Resolve(context.Background(), alpine(t))
	if res.Outcome == OutcomeDigest || tokenAsked.Load() != 0 {
		t.Fatalf("outcome %s, %d token requests to an http realm; want neither", res.Outcome, tokenAsked.Load())
	}

	// The same realm for a plain-http registry is fine.
	f := newFakeRegistry(t)
	if res := f.client().Resolve(context.Background(), alpine(t)); res.Outcome != OutcomeDigest {
		t.Fatalf("plain-http registry with an http realm: %s (%s)", res.Outcome, res.Reason)
	}
}

// A digest header from another host is that host's answer, not the
// registry's.
func TestResolveDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", indexDigest)
	}))
	defer other.Close()
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer reg.Close()
	c := &Client{Endpoint: func(string) string { return reg.URL }}
	if res := c.Resolve(context.Background(), alpine(t)); res.Outcome == OutcomeDigest {
		t.Fatalf("took %s from the host it was redirected to", res.Digest)
	}
}

func indexServer(t *testing.T, body string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", MediaOCIIndex)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func digestOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestIndexListsTheChildrenOfTheDigestAskedFor(t *testing.T) {
	body := fmt.Sprintf(`{"mediaType":%q,"manifests":[{"digest":%q},{"digest":%q}]}`, MediaOCIIndex, platformDigest, indexDigest)
	srv := indexServer(t, body, nil)
	c := &Client{Endpoint: func(string) string { return srv.URL }}
	res := c.Index(context.Background(), alpine(t), digestOf(body))
	if res.Outcome != OutcomeDigest || len(res.Children) != 2 || res.Children[0] != platformDigest {
		t.Fatalf("Index = %+v", res)
	}

	// Content that is not what the digest names is not listed.
	if res := c.Index(context.Background(), alpine(t), indexDigest); res.Outcome == OutcomeDigest {
		t.Fatalf("listed children of content that does not hash to %s: %+v", indexDigest, res)
	}
}

func TestCheckerIndexIsCached(t *testing.T) {
	var hits atomic.Int32
	body := fmt.Sprintf(`{"manifests":[{"digest":%q}]}`, platformDigest)
	srv := indexServer(t, body, &hits)
	cache := filepath.Join(t.TempDir(), "digests.json")
	newChecker := func() *Checker {
		return &Checker{Client: &Client{Endpoint: func(string) string { return srv.URL }}, CachePath: cache, Now: checkNow}
	}
	if res := newChecker().Index(context.Background(), alpine(t), digestOf(body)); len(res.Children) != 1 {
		t.Fatalf("first Index = %+v", res)
	}
	res := newChecker().Index(context.Background(), alpine(t), digestOf(body))
	if !res.Cached || len(res.Children) != 1 || hits.Load() != 1 {
		t.Fatalf("second Index = %+v after %d requests, want one request and a cached answer", res, hits.Load())
	}
}

// A new Checker, as the next doctor run makes, answers from the file.
func TestCheckReadsTheFileAnotherRunWrote(t *testing.T) {
	f := newFakeRegistry(t)
	cache := filepath.Join(t.TempDir(), "registry", "digests.json")
	(&Checker{Client: f.client(), CachePath: cache, Now: checkNow}).Check(context.Background(), []Ref{alpine(t)})
	asked := f.heads.Load()
	got := (&Checker{Client: f.client(), CachePath: cache, Now: checkNow.Add(time.Minute)}).Check(context.Background(), []Ref{alpine(t)})
	if r := got[alpine(t).String()]; !r.Cached || r.Digest != indexDigest || f.heads.Load() != asked {
		t.Fatalf("second run = %+v after %d more requests", r, f.heads.Load()-asked)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Dir(cache))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("cache dir = %v %v, want 0700", info.Mode().Perm(), err)
		}
	}
}

// Recheck is what doctor uses for a cached answer that disagrees with what
// runs: it asks whatever the cache holds, and keeps the fresh answer.
func TestRecheckAsksDespiteTheCache(t *testing.T) {
	f := newFakeRegistry(t)
	c := &Checker{Client: f.client(), CachePath: filepath.Join(t.TempDir(), "d.json"), Now: checkNow}
	c.Check(context.Background(), []Ref{alpine(t)})
	asked := f.heads.Load()
	got := c.Recheck(context.Background(), []Ref{alpine(t)})
	if r := got[alpine(t).String()]; r.Cached || f.heads.Load() == asked {
		t.Fatalf("Recheck = %+v without asking the registry", r)
	}
}

func writeCache(t *testing.T, path string, f cacheFile) {
	t.Helper()
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Entries the cache must not answer from: one dated in the future, one past
// its TTL, one claiming a digest with no digest, and any entry in a file of
// another version.
func TestCheckRefusesCacheEntriesItCannotTrust(t *testing.T) {
	key := alpine(t).String()
	for name, file := range map[string]cacheFile{
		"future":    {Version: cacheVersion, Entries: map[string]Result{key: {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow.Add(time.Hour)}}},
		"expired":   {Version: cacheVersion, Entries: map[string]Result{key: {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow.Add(-7 * time.Hour)}}},
		"no digest": {Version: cacheVersion, Entries: map[string]Result{key: {Outcome: OutcomeDigest, CheckedAt: checkNow}}},
		"version":   {Version: cacheVersion + 1, Entries: map[string]Result{key: {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow}}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeRegistry(t)
			path := filepath.Join(t.TempDir(), "digests.json")
			writeCache(t, path, file)
			got := (&Checker{Client: f.client(), CachePath: path, Now: checkNow}).Check(context.Background(), []Ref{alpine(t)})
			if r := got[key]; r.Cached || r.Digest != indexDigest {
				t.Fatalf("answered %+v from an entry it should not trust", r)
			}
		})
	}
}

// What is saved is what can be answered from; the rest is dropped, so the
// file does not grow with every tag ever run.
func TestSaveDropsEntriesPastTheirTTL(t *testing.T) {
	f := newFakeRegistry(t)
	path := filepath.Join(t.TempDir(), "digests.json")
	writeCache(t, path, cacheFile{Version: cacheVersion, Entries: map[string]Result{
		"docker.io/library/old:1":    {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow.Add(-7 * time.Hour)},
		"docker.io/library/future:1": {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow.Add(time.Hour)},
		"docker.io/library/kept:1":   {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow.Add(-time.Hour)},
	}})
	(&Checker{Client: f.client(), CachePath: path, Now: checkNow}).Check(context.Background(), []Ref{alpine(t)})
	data, _ := os.ReadFile(path)
	var got cacheFile
	_ = json.Unmarshal(data, &got)
	if _, ok := got.Entries["docker.io/library/old:1"]; ok {
		t.Error("an expired entry was kept")
	}
	if _, ok := got.Entries["docker.io/library/future:1"]; ok {
		t.Error("a future-dated entry was kept")
	}
	if _, ok := got.Entries["docker.io/library/kept:1"]; !ok {
		t.Error("an entry inside its TTL was dropped")
	}
}

// Credentials and "no such tag" are answers the registry will give again;
// an answer this package could not use says nothing about the next one.
func TestCheckCachesAuthAndNotFoundButNotErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		cached bool
	}{
		{http.StatusForbidden, true},
		{http.StatusNotFound, true},
		{http.StatusInternalServerError, false},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "digests.json")
			mk := func() *Checker {
				return &Checker{Client: &Client{Endpoint: func(string) string { return srv.URL }}, CachePath: path, Now: checkNow}
			}
			mk().Check(context.Background(), []Ref{alpine(t)})
			mk().Check(context.Background(), []Ref{alpine(t)})
			if cached := hits.Load() == 1; cached != tc.cached {
				t.Fatalf("%d requests over two runs; cached = %v, want %v", hits.Load(), cached, tc.cached)
			}
		})
	}
}

// After one tag on a registry gets no answer, the others on it are not each
// given a timeout — and a tag on a different registry is still asked.
func TestDownRegistryShortcutIsPerRegistry(t *testing.T) {
	var downHits atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downHits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer down.Close()
	up := newFakeRegistry(t)

	var refs []Ref
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		ref, _ := ParseReference("down.example/" + n + ":1")
		refs = append(refs, ref)
	}
	other, _ := ParseReference("up.example/library/alpine:3.20")
	refs = append(refs, other)

	c := &Checker{
		Client: &Client{Endpoint: func(reg string) string {
			if reg == "down.example" {
				return down.URL
			}
			return up.srv.URL
		}},
		Now:     checkNow,
		Workers: 1,
	}
	got := c.Check(context.Background(), refs)
	if downHits.Load() != 1 {
		t.Fatalf("a rate-limited registry was asked %d times in one run", downHits.Load())
	}
	if got[other.String()].Outcome != OutcomeDigest {
		t.Fatalf("another registry was not asked: %+v", got[other.String()])
	}
}

func TestNegativeTTLTakesTheDefault(t *testing.T) {
	if (&Checker{TTL: -time.Hour}).ttl() != DefaultTTL {
		t.Fatal("a negative TTL was used as given")
	}
}

// A cache that cannot be written is said, not swallowed: otherwise every
// run asks every registry again and the first sign is a 429.
func TestSaveErrorIsReported(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced here")
	}
	f := newFakeRegistry(t)
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c := &Checker{Client: f.client(), CachePath: filepath.Join(dir, "digests.json"), Now: checkNow}
	c.Check(context.Background(), []Ref{alpine(t)})
	if c.SaveError() == nil {
		t.Fatal("writing into a read-only directory reported no error")
	}
	ro := &Checker{Client: f.client(), CachePath: filepath.Join(dir, "digests.json"), Now: checkNow, ReadOnly: true}
	ro.Check(context.Background(), []Ref{alpine(t)})
	if ro.SaveError() != nil {
		t.Fatalf("a read-only checker reported a save error: %v", ro.SaveError())
	}
}
