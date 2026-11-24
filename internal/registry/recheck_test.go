package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A recheck that meets a 429 says nothing new about the tag, and must not
// throw away the digest the cache already holds for it.
func TestARecheckThatFailsKeepsTheCachedDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "digests.json")
	key := alpine(t).String()
	writeCache(t, path, cacheFile{Version: cacheVersion, Entries: map[string]Result{
		key: {Outcome: OutcomeDigest, Digest: platformDigest, CheckedAt: checkNow.Add(-time.Hour)},
	}})
	c := &Checker{Client: &Client{Endpoint: func(string) string { return srv.URL }}, CachePath: path, Now: checkNow}
	if got := c.Recheck(context.Background(), []Ref{alpine(t)}); got[key].Outcome != OutcomeRateLimited {
		t.Fatalf("recheck = %+v, want the 429 returned", got[key])
	}
	next := (&Checker{CachePath: path, Now: checkNow.Add(time.Minute)}).Check(context.Background(), []Ref{alpine(t)})
	if r := next[key]; !r.Cached || r.Digest != platformDigest {
		t.Fatalf("after a failed recheck the cache answers %+v, want the digest it held", r)
	}
}

// An index that lists nothing is still an answer; with omitempty dropping
// the empty list it looked unfetched and was fetched on every run.
func TestAnEmptyIndexListingIsCached(t *testing.T) {
	var hits atomic.Int32
	body := `{"manifests":[]}`
	srv := indexServer(t, body, &hits)
	cache := filepath.Join(t.TempDir(), "digests.json")
	for i := 0; i < 3; i++ {
		c := &Checker{Client: &Client{Endpoint: func(string) string { return srv.URL }}, CachePath: cache, Now: checkNow}
		if res := c.Index(context.Background(), alpine(t), digestOf(body)); res.Outcome != OutcomeDigest {
			t.Fatalf("Index = %+v", res)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("an empty index was fetched %d times", hits.Load())
	}
}

// Confirm is what lets a behind container go unasked until the TTL: it is
// kept in the file, for the next run's Checker.
func TestConfirmIsKeptInTheFile(t *testing.T) {
	f := newFakeRegistry(t)
	path := filepath.Join(t.TempDir(), "digests.json")
	c := &Checker{Client: f.client(), CachePath: path, Now: checkNow}
	c.Check(context.Background(), []Ref{alpine(t)})
	c.Confirm(alpine(t), []string{platformDigest})
	got := (&Checker{CachePath: path, Now: checkNow}).Check(context.Background(), []Ref{alpine(t)})
	if r := got[alpine(t).String()]; len(r.Confirmed) != 1 || r.Confirmed[0] != platformDigest {
		t.Fatalf("next run reads %+v", r)
	}
}
