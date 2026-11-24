package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Outcome is what a registry said about a tag.
type Outcome string

const (
	// OutcomeDigest: the registry named the digest the tag points at.
	OutcomeDigest Outcome = "digest"
	// OutcomeAuth: the registry would not answer without credentials.
	OutcomeAuth Outcome = "auth"
	// OutcomeRateLimited: the registry answered 429.
	OutcomeRateLimited Outcome = "rate_limited"
	// OutcomeNotFound: the registry has no such tag, or will not say so to
	// an anonymous caller.
	OutcomeNotFound Outcome = "not_found"
	// OutcomeUnreachable: no answer at all, or not in time.
	OutcomeUnreachable Outcome = "unreachable"
	// OutcomeError: an answer this package could not use.
	OutcomeError Outcome = "error"
)

// Result is one lookup.
type Result struct {
	Outcome Outcome `json:"outcome"`
	Digest  string  `json:"digest,omitempty"`
	// MediaType is the manifest type the registry answered with. An index
	// or manifest list is the one whose children can be asked for.
	MediaType string `json:"media_type,omitempty"`
	// Children are the manifest digests an index lists, filled in only by
	// Index. Listed says Index filled them in, so an index that lists
	// nothing is still an answer rather than one to fetch again.
	Children []string `json:"children,omitempty"`
	Listed   bool     `json:"listed,omitempty"`
	// Confirmed are the running digests this answer was found, when it was
	// fresh, to disagree with. A cached answer that still disagrees with the
	// same running image needs no second look; one that disagrees with a
	// different running image may be one the operator has since acted on.
	Confirmed []string `json:"confirmed,omitempty"`
	// Reason is the detail behind an outcome other than OutcomeDigest.
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	// Cached is true when the answer came from the cache file.
	Cached bool `json:"-"`
}

// IsIndex reports whether the answer is a multi-platform index.
func (r Result) IsIndex() bool {
	return r.MediaType == MediaOCIIndex || r.MediaType == MediaDockerList
}

// The two multi-platform index media types.
const (
	MediaOCIIndex   = "application/vnd.oci.image.index.v1+json"
	MediaDockerList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// manifestAccept asks for whichever of the four manifest shapes the tag
// points at. The index types are what make a multi-arch tag answer with the
// digest of its list rather than of one platform's manifest: that list digest
// is the one docker records in RepoDigests on pull, so asking for anything
// narrower would make every multi-arch image look updated.
var manifestAccept = strings.Join([]string{
	MediaOCIIndex,
	MediaDockerList,
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
}, ", ")

// Client resolves tags to digests.
type Client struct {
	HTTP *http.Client
	// Endpoint maps a registry to the base URL it answers on. Empty takes
	// https, with Docker Hub's API host for docker.io. Tests point it at an
	// httptest server.
	Endpoint  func(registry string) string
	UserAgent string

	mu     sync.Mutex
	tokens map[string]string
}

func (c *Client) endpoint(registry string) string {
	if c.Endpoint != nil {
		return c.Endpoint(registry)
	}
	if registry == DockerHub {
		return "https://registry-1.docker.io"
	}
	return "https://" + registry
}

var errCrossHostRedirect = errors.New("redirected to another host")

// httpClient refuses a redirect to another host. A digest header is only
// worth trusting from the registry the reference names; a redirect elsewhere
// would let that other host answer for it.
func (c *Client) httpClient() *http.Client {
	var hc http.Client
	if c.HTTP != nil {
		hc = *c.HTTP
	} else {
		hc = http.Client{Timeout: 5 * time.Second}
	}
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Host != via[0].URL.Host {
			return errCrossHostRedirect
		}
		return nil
	}
	return &hc
}

// Resolve asks the registry which digest ref's tag points at. The request is
// a HEAD, which Docker Hub does not count against the anonymous pull limit,
// retried once with a token when the registry answers with a Bearer challenge.
func (c *Client) Resolve(ctx context.Context, ref Ref) Result {
	resp, res, ok := c.request(ctx, http.MethodHead, ref, ref.Tag)
	if !ok {
		return res
	}
	_ = resp.Body.Close()
	return manifestResult(ref, resp)
}

// Index fetches the index digest names and lists the manifests in it. It is
// a GET, which Docker Hub counts as a pull, so it is asked only when a
// running image's digest is not the index's: an image pulled by a platform
// manifest's digest and then tagged runs one of the children.
//
// The body is checked against the digest it was asked for, so what is
// listed is the content that digest names and nothing else.
func (c *Client) Index(ctx context.Context, ref Ref, digest string) Result {
	resp, res, ok := c.request(ctx, http.MethodGet, ref, digest)
	if !ok {
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return manifestResult(ref, resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return transportResult(err)
	}
	sum := sha256.Sum256(body)
	if "sha256:"+hex.EncodeToString(sum[:]) != digest {
		return Result{Outcome: OutcomeError, Reason: ref.Registry + " answered for " + shortForm(digest) + " with content that is not it"}
	}
	var index struct {
		MediaType string `json:"mediaType"`
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	if json.Unmarshal(body, &index) != nil {
		return Result{Outcome: OutcomeError, Reason: ref.Registry + " sent an index that could not be read"}
	}
	out := Result{Outcome: OutcomeDigest, Digest: digest, MediaType: resp.Header.Get("Content-Type"), Children: []string{}, Listed: true}
	for _, m := range index.Manifests {
		if m.Digest != "" {
			out.Children = append(out.Children, m.Digest)
		}
	}
	return out
}

func shortForm(digest string) string {
	if len(digest) > 19 {
		return digest[:19]
	}
	return digest
}

// request sends one manifest request, answering a Bearer challenge once. ok
// is false when res is the final answer.
func (c *Client) request(ctx context.Context, method string, ref Ref, reference string) (*http.Response, Result, bool) {
	base := c.endpoint(ref.Registry)
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", base, ref.Repository, reference)

	scopeKey := ref.Registry + "/" + ref.Repository
	c.mu.Lock()
	token := c.tokens[scopeKey]
	c.mu.Unlock()

	resp, err := c.send(ctx, method, manifestURL, token)
	if err != nil {
		return nil, requestErrResult(ref, err), false
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		params, ok := parseBearer(resp.Header.Get("WWW-Authenticate"))
		if !ok {
			return nil, Result{Outcome: OutcomeAuth, Reason: ref.Registry + " asks for credentials"}, false
		}
		token, res := c.token(ctx, params, ref, base)
		if token == "" {
			return nil, res, false
		}
		c.mu.Lock()
		if c.tokens == nil {
			c.tokens = map[string]string{}
		}
		c.tokens[scopeKey] = token
		c.mu.Unlock()
		resp, err = c.send(ctx, method, manifestURL, token)
		if err != nil {
			return nil, requestErrResult(ref, err), false
		}
	}
	return resp, Result{}, true
}

func (c *Client) send(ctx context.Context, method, rawURL, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", manifestAccept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if method == http.MethodHead {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	}
	return resp, nil
}

func requestErrResult(ref Ref, err error) Result {
	if errors.Is(err, errCrossHostRedirect) {
		return Result{Outcome: OutcomeError, Reason: ref.Registry + " redirected to another host, whose answer is not taken for it"}
	}
	return transportResult(err)
}

func manifestResult(ref Ref, resp *http.Response) Result {
	switch resp.StatusCode {
	case http.StatusOK:
		digest := resp.Header.Get("Docker-Content-Digest")
		if !strings.HasPrefix(digest, "sha256:") {
			return Result{Outcome: OutcomeError, Reason: ref.Registry + " answered without a manifest digest"}
		}
		mediaType, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
		return Result{Outcome: OutcomeDigest, Digest: digest, MediaType: strings.TrimSpace(mediaType)}
	case http.StatusUnauthorized, http.StatusForbidden:
		return Result{Outcome: OutcomeAuth, Reason: ref.Registry + " would not show " + ref.Repository + " without credentials"}
	case http.StatusNotFound:
		return Result{Outcome: OutcomeNotFound, Reason: ref.Registry + " has no tag " + ref.Tag + " for " + ref.Repository + " that it will show without credentials"}
	case http.StatusTooManyRequests:
		return Result{Outcome: OutcomeRateLimited, Reason: ref.Registry + " is rate limiting this address"}
	default:
		return Result{Outcome: OutcomeError, Reason: fmt.Sprintf("%s answered %s", ref.Registry, resp.Status)}
	}
}

func transportResult(err error) Result {
	reason := err.Error()
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		reason = urlErr.Err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "no answer in time"
	}
	return Result{Outcome: OutcomeUnreachable, Reason: reason}
}

// token fetches an anonymous token from the realm a Bearer challenge names.
// Docker Hub's challenge points at auth.docker.io and ghcr.io's at its own
// /token, so one path covers both and any registry that follows the spec.
//
// The realm has to be https. A registry served over plain http, such as one
// on localhost:5000, may name an http realm; one served over https may not
// send this request anywhere it can be read in transit.
func (c *Client) token(ctx context.Context, params map[string]string, ref Ref, base string) (string, Result) {
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Host == "" {
		return "", Result{Outcome: OutcomeError, Reason: ref.Registry + " sent a token challenge with no usable realm"}
	}
	plainRegistry := strings.HasPrefix(base, "http://")
	if realm.Scheme != "https" && (realm.Scheme != "http" || !plainRegistry) {
		return "", Result{Outcome: OutcomeError, Reason: ref.Registry + " asked for a token from " + realm.Scheme + "://" + realm.Host + ", which is not https"}
	}
	q := realm.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + ref.Repository + ":pull"
	}
	q.Set("scope", scope)
	realm.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", Result{Outcome: OutcomeError, Reason: err.Error()}
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", requestErrResult(ref, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", Result{Outcome: OutcomeAuth, Reason: ref.Registry + " gives no anonymous token for " + ref.Repository}
	case http.StatusTooManyRequests:
		return "", Result{Outcome: OutcomeRateLimited, Reason: ref.Registry + " is rate limiting this address"}
	default:
		return "", Result{Outcome: OutcomeError, Reason: fmt.Sprintf("%s token request answered %s", ref.Registry, resp.Status)}
	}

	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", Result{Outcome: OutcomeError, Reason: ref.Registry + " sent a token that could not be read"}
	}
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", Result{Outcome: OutcomeAuth, Reason: ref.Registry + " gives no anonymous token for " + ref.Repository}
	}
	return token, Result{}
}

// parseBearer reads a `Bearer realm="…",service="…",scope="…"` challenge.
func parseBearer(header string) (map[string]string, bool) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return nil, false
	}
	params := map[string]string{}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ,")
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		var value string
		if strings.HasPrefix(after, `"`) {
			end := strings.Index(after[1:], `"`)
			if end < 0 {
				return nil, false
			}
			value = after[1 : end+1]
			rest = after[end+2:]
		} else {
			value, rest, _ = strings.Cut(after, ",")
		}
		params[key] = value
	}
	if params["realm"] == "" {
		return nil, false
	}
	return params, true
}
