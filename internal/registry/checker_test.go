package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

var checkNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func TestCheckReusesAnAnswerWithinTheTTL(t *testing.T) {
	f := newFakeRegistry(t)
	cache := filepath.Join(t.TempDir(), "registry", "digests.json")
	c := Checker{Client: f.client(), CachePath: cache, TTL: time.Hour, Now: checkNow}

	first := c.Check(context.Background(), []Ref{alpine(t)})
	if got := first[alpine(t).String()]; got.Digest != indexDigest || got.Cached {
		t.Fatalf("first check = %+v, want a fresh index digest", got)
	}
	asked := f.heads.Load()

	c.Now = checkNow.Add(59 * time.Minute)
	second := c.Check(context.Background(), []Ref{alpine(t)})
	if got := second[alpine(t).String()]; got.Digest != indexDigest || !got.Cached || !got.CheckedAt.Equal(checkNow) {
		t.Fatalf("second check = %+v, want the cached answer from the first", got)
	}
	if f.heads.Load() != asked {
		t.Fatalf("the registry was asked again inside the TTL")
	}

	c.Now = checkNow.Add(61 * time.Minute)
	third := c.Check(context.Background(), []Ref{alpine(t)})
	if got := third[alpine(t).String()]; got.Cached {
		t.Fatalf("third check = %+v, want a fresh answer past the TTL", got)
	}
	if f.heads.Load() == asked {
		t.Fatalf("the registry was not asked again past the TTL")
	}

	info, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("cache is %04o, want it private", info.Mode().Perm())
	}
}

// A 429 is the one answer that must be remembered: asking again is how a
// rate limit is kept rather than waited out.
func TestCheckRemembersARateLimit(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := Checker{
		Client:    &Client{Endpoint: func(string) string { return srv.URL }},
		CachePath: filepath.Join(t.TempDir(), "digests.json"),
		Now:       checkNow,
	}
	nginx, _ := ParseReference("nginx:latest")
	refs := []Ref{alpine(t), nginx}

	got := c.Check(context.Background(), refs)
	for _, ref := range refs {
		if got[ref.String()].Outcome != OutcomeRateLimited {
			t.Fatalf("%s = %+v, want rate_limited", ref, got[ref.String()])
		}
	}
	after := hits.Load()
	c.Now = checkNow.Add(time.Minute)
	c.Check(context.Background(), refs)
	if hits.Load() != after {
		t.Fatalf("a rate-limited registry was asked again a minute later")
	}
}

// A registry that did not answer may answer in a minute; caching that would
// say "unreachable" for six hours after the network came back.
func TestCheckDoesNotRememberAnUnreachableRegistry(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	cache := filepath.Join(t.TempDir(), "digests.json")
	c := Checker{Client: &Client{Endpoint: func(string) string { return url }}, CachePath: cache, Now: checkNow}
	got := c.Check(context.Background(), []Ref{alpine(t)})
	if got[alpine(t).String()].Outcome != OutcomeUnreachable {
		t.Fatalf("got %+v, want unreachable", got)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("a run with nothing to keep wrote the cache: %v", err)
	}

	f := newFakeRegistry(t)
	c.Client = f.client()
	c.Now = checkNow.Add(time.Minute)
	got = c.Check(context.Background(), []Ref{alpine(t)})
	if got[alpine(t).String()].Digest != indexDigest {
		t.Fatalf("got %+v, want the registry asked again once it answers", got)
	}
}

// Network off must not make doctor slow: one deadline for the whole check,
// not a timeout per image.
func TestCheckIsBoundedByOneDeadline(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	var refs []Ref
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		ref, _ := ParseReference(name + ":latest")
		refs = append(refs, ref)
	}
	c := Checker{
		Client:   &Client{Endpoint: func(string) string { return srv.URL }},
		Now:      checkNow,
		Deadline: 200 * time.Millisecond,
		Workers:  2,
	}
	start := time.Now()
	got := c.Check(context.Background(), refs)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("check took %s with a 200ms deadline", elapsed)
	}
	for _, ref := range refs {
		if got[ref.String()].Outcome != OutcomeUnreachable {
			t.Fatalf("%s = %+v, want unreachable", ref, got[ref.String()])
		}
	}
}

func TestCheckSkipsPinnedReferencesAndAsksOncePerTag(t *testing.T) {
	f := newFakeRegistry(t)
	c := Checker{Client: f.client(), Now: checkNow}
	pinned, _ := ParseReference("alpine@" + indexDigest)
	got := c.Check(context.Background(), []Ref{alpine(t), alpine(t), pinned})
	if _, ok := got[pinned.String()]; ok {
		t.Fatal("a pinned reference was looked up")
	}
	if f.tokens.Load() != 1 {
		t.Fatalf("token requests = %d, want one for a tag listed twice", f.tokens.Load())
	}
}

func TestCheckReadsACorruptCacheAsEmpty(t *testing.T) {
	f := newFakeRegistry(t)
	cache := filepath.Join(t.TempDir(), "digests.json")
	if err := os.WriteFile(cache, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Checker{Client: f.client(), CachePath: cache, Now: checkNow}
	if got := c.Check(context.Background(), []Ref{alpine(t)}); got[alpine(t).String()].Digest != indexDigest {
		t.Fatalf("got %+v", got)
	}
}
