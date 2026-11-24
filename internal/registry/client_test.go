package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	indexDigest    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	platformDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// fakeRegistry answers like Docker Hub and ghcr.io do: a 401 with a Bearer
// challenge, a token from the realm, then the manifest. It serves a
// multi-arch tag, and answers with the index digest only to a client that
// asks for an index — what a real registry does, and the reason a client
// that forgets the index media types sees every multi-arch image as updated.
type fakeRegistry struct {
	srv       *httptest.Server
	heads     atomic.Int32
	tokens    atomic.Int32
	manifest  func(w http.ResponseWriter, r *http.Request) bool
	challenge string
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokens.Add(1)
		if r.URL.Query().Get("scope") != "repository:library/alpine:pull" || r.URL.Query().Get("service") != "fake" {
			http.Error(w, "bad scope", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "anonymous only", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, `{"token":"anon-token"}`)
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		f.heads.Add(1)
		if r.Method != http.MethodHead {
			http.Error(w, "HEAD only", http.StatusMethodNotAllowed)
			return
		}
		if f.manifest != nil && f.manifest(w, r) {
			return
		}
		if r.Header.Get("Authorization") != "Bearer anon-token" {
			challenge := f.challenge
			if challenge == "" {
				challenge = fmt.Sprintf(`Bearer realm="%s/token",service="fake",scope="repository:library/alpine:pull"`, f.srv.URL)
			}
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v2/library/alpine/manifests/3.20" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") ||
			strings.Contains(r.Header.Get("Accept"), "application/vnd.docker.distribution.manifest.list.v2+json") {
			w.Header().Set("Docker-Content-Digest", indexDigest)
		} else {
			w.Header().Set("Docker-Content-Digest", platformDigest)
		}
		w.WriteHeader(http.StatusOK)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRegistry) client() *Client {
	return &Client{
		HTTP:     &http.Client{Timeout: 2 * time.Second},
		Endpoint: func(string) string { return f.srv.URL },
	}
}

func alpine(t *testing.T) Ref {
	t.Helper()
	ref, err := ParseReference("alpine:3.20")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestResolveFollowsTheBearerChallengeAnonymously(t *testing.T) {
	f := newFakeRegistry(t)
	res := f.client().Resolve(context.Background(), alpine(t))
	if res.Outcome != OutcomeDigest {
		t.Fatalf("outcome = %s (%s), want a digest", res.Outcome, res.Reason)
	}
	if f.tokens.Load() != 1 || f.heads.Load() != 2 {
		t.Fatalf("token requests %d, manifest requests %d; want 1 and 2", f.tokens.Load(), f.heads.Load())
	}
}

// The digest docker records in RepoDigests for a multi-arch pull is the
// index's. Asking for anything narrower gets the platform manifest's digest,
// which never matches and makes every multi-arch image look updated.
func TestResolveAnswersWithTheIndexDigestForAMultiArchTag(t *testing.T) {
	f := newFakeRegistry(t)
	res := f.client().Resolve(context.Background(), alpine(t))
	if res.Digest != indexDigest {
		t.Fatalf("digest = %s, want the index digest %s", res.Digest, indexDigest)
	}
}

func TestResolveSaysRateLimited(t *testing.T) {
	f := newFakeRegistry(t)
	f.manifest = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	if res := f.client().Resolve(context.Background(), alpine(t)); res.Outcome != OutcomeRateLimited {
		t.Fatalf("outcome = %s, want rate_limited", res.Outcome)
	}
}

func TestResolveSaysRateLimitedByTheTokenEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{Endpoint: func(string) string { return srv.URL }}
	if res := c.Resolve(context.Background(), alpine(t)); res.Outcome != OutcomeRateLimited {
		t.Fatalf("outcome = %s (%s), want rate_limited", res.Outcome, res.Reason)
	}
}

func TestResolveSaysCredentialsWhenThereIsNoBearerChallenge(t *testing.T) {
	for name, challenge := range map[string]string{
		"none":  "",
		"basic": `Basic realm="private"`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if challenge != "" {
					w.Header().Set("WWW-Authenticate", challenge)
				}
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer srv.Close()
			c := &Client{Endpoint: func(string) string { return srv.URL }}
			res := c.Resolve(context.Background(), alpine(t))
			if res.Outcome != OutcomeAuth {
				t.Fatalf("outcome = %s (%s), want auth", res.Outcome, res.Reason)
			}
		})
	}
}

// A private repository on ghcr.io hands out an anonymous token and then
// refuses it. That is "needs credentials", not a broken registry.
func TestResolveSaysCredentialsWhenTheTokenIsRefused(t *testing.T) {
	f := newFakeRegistry(t)
	f.manifest = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") == "" {
			return false
		}
		w.WriteHeader(http.StatusForbidden)
		return true
	}
	if res := f.client().Resolve(context.Background(), alpine(t)); res.Outcome != OutcomeAuth {
		t.Fatalf("outcome = %s (%s), want auth", res.Outcome, res.Reason)
	}
}

func TestResolveSaysNotFound(t *testing.T) {
	f := newFakeRegistry(t)
	ref := alpine(t)
	ref.Tag = "no-such-tag"
	if res := f.client().Resolve(context.Background(), ref); res.Outcome != OutcomeNotFound {
		t.Fatalf("outcome = %s, want not_found", res.Outcome)
	}
}

func TestResolveNeedsADigestHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Client{Endpoint: func(string) string { return srv.URL }}
	if res := c.Resolve(context.Background(), alpine(t)); res.Outcome != OutcomeError {
		t.Fatalf("outcome = %s, want error", res.Outcome)
	}
}

func TestResolveSaysUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := &Client{Endpoint: func(string) string { return url }}
	if res := c.Resolve(context.Background(), alpine(t)); res.Outcome != OutcomeUnreachable {
		t.Fatalf("outcome = %s (%s), want unreachable", res.Outcome, res.Reason)
	}
}

func TestParseBearer(t *testing.T) {
	p, ok := parseBearer(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`)
	if !ok || p["realm"] != "https://auth.docker.io/token" || p["service"] != "registry.docker.io" || p["scope"] != "repository:library/nginx:pull" {
		t.Fatalf("parseBearer = %v %v", p, ok)
	}
	if _, ok := parseBearer(`Basic realm="x"`); ok {
		t.Fatal("a Basic challenge is not a Bearer one")
	}
	if _, ok := parseBearer(`Bearer service="x"`); ok {
		t.Fatal("a challenge without a realm has nowhere to ask")
	}
}
