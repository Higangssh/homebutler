package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultTTL is how long an answer is reused. Docker Hub counts anonymous
// requests per address over six hours, so a doctor run from cron every few
// minutes asks it about each tag about once in that window.
const DefaultTTL = 6 * time.Hour

// Checker resolves many references at once, through a cache file.
//
// A Checker is used for one doctor run: it loads the cache once, and every
// call after that reads and adds to the same copy.
type Checker struct {
	Client *Client
	// CachePath is the JSON file answers are kept in. Empty keeps nothing.
	CachePath string
	TTL       time.Duration
	Now       time.Time
	// Deadline bounds each call, so a registry that never answers costs this
	// much once and not a timeout per image.
	Deadline time.Duration
	// Workers bounds how many requests are in flight.
	Workers int
	// ReadOnly reads the cache and never writes it, for a caller that has
	// promised to change nothing on disk.
	ReadOnly bool

	mu      sync.Mutex
	cache   *cacheFile
	saveErr error
}

// cacheFile is the on-disk shape. Version lets a later release change it
// and read an old file as empty rather than as wrong.
type cacheFile struct {
	Version int               `json:"version"`
	Entries map[string]Result `json:"entries"`
}

const cacheVersion = 1

// cacheable reports whether an outcome is worth remembering. A registry that
// did not answer may answer in a minute, so that is asked again; one that
// said "credentials" or "slow down" will say it again, and asking again is
// how a rate limit is kept rather than waited out. An answer that could not
// be used is not kept either: it says nothing about the next one.
func cacheable(o Outcome) bool {
	switch o {
	case OutcomeDigest, OutcomeAuth, OutcomeRateLimited, OutcomeNotFound:
		return true
	}
	return false
}

func (c *Checker) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

func (c *Checker) ttl() time.Duration {
	if c.TTL <= 0 {
		return DefaultTTL
	}
	return c.TTL
}

func (c *Checker) client() *Client {
	if c.Client == nil {
		c.Client = &Client{}
	}
	return c.Client
}

func (c *Checker) withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	d := c.Deadline
	if d <= 0 {
		d = 10 * time.Second
	}
	return context.WithTimeout(ctx, d)
}

// usable reports whether a cached entry can be answered from. One dated in
// the future came from a clock that was wrong, and would otherwise be
// trusted until the clock caught up with it.
func usable(e Result, now time.Time, ttl time.Duration) bool {
	if !cacheable(e.Outcome) || e.CheckedAt.After(now) || now.Sub(e.CheckedAt) >= ttl {
		return false
	}
	return e.Outcome != OutcomeDigest || e.Digest != ""
}

// Check resolves each distinct reference once, answering from the cache
// where it can. Pinned references are not asked about: there is no tag for
// them to fall behind.
func (c *Checker) Check(ctx context.Context, refs []Ref) map[string]Result {
	return c.resolve(ctx, refs, false)
}

// Recheck asks the registry about refs whatever the cache holds. A cached
// answer that disagrees with what runs may be one the operator has since
// acted on, and is asked again before anything is said about it.
func (c *Checker) Recheck(ctx context.Context, refs []Ref) map[string]Result {
	return c.resolve(ctx, refs, true)
}

func (c *Checker) resolve(ctx context.Context, refs []Ref, bypass bool) map[string]Result {
	now, ttl := c.now(), c.ttl()
	cache := c.loaded()

	out := map[string]Result{}
	var todo []Ref
	seen := map[string]bool{}
	c.mu.Lock()
	for _, ref := range refs {
		key := ref.String()
		if ref.Pinned() || seen[key] {
			continue
		}
		seen[key] = true
		if hit, ok := cache.Entries[key]; ok && !bypass && usable(hit, now, ttl) {
			hit.Cached = true
			out[key] = hit
			continue
		}
		todo = append(todo, ref)
	}
	c.mu.Unlock()
	if len(todo) == 0 {
		return out
	}

	ctx, cancel := c.withDeadline(ctx)
	defer cancel()

	workers := c.Workers
	if workers <= 0 {
		workers = 4
	}
	client := c.client()
	var (
		mu sync.Mutex
		// down remembers a registry that did not answer, or answered 429,
		// so the other tags on it are not each given the full timeout or
		// spent against the same limit. Another registry is still asked.
		down = map[string]Result{}
		// added counts answers worth keeping, so a run that got none leaves
		// the file as it was.
		added int
		wg    sync.WaitGroup
		jobs  = make(chan Ref)
	)
	for i := 0; i < workers && i < len(todo); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ref := range jobs {
				mu.Lock()
				failed, isDown := down[ref.Registry]
				mu.Unlock()
				var res Result
				if isDown {
					res = failed
				} else {
					res = client.Resolve(ctx, ref)
				}
				res.CheckedAt = now
				mu.Lock()
				if res.Outcome == OutcomeUnreachable || res.Outcome == OutcomeRateLimited {
					down[ref.Registry] = res
				}
				out[ref.String()] = res
				if cacheable(res.Outcome) {
					c.mu.Lock()
					// A recheck that met a 429 or a credentials wall does
					// not replace a digest the cache already holds: that
					// digest is still the best answer there is.
					prev, had := cache.Entries[ref.String()]
					if res.Outcome == OutcomeDigest || !had || !usable(prev, now, ttl) || prev.Outcome != OutcomeDigest {
						cache.Entries[ref.String()] = res
						added++
					}
					c.mu.Unlock()
				}
				mu.Unlock()
			}
		}()
	}
	for _, ref := range todo {
		jobs <- ref
	}
	close(jobs)
	wg.Wait()

	if added > 0 {
		c.save()
	}
	return out
}

// Index lists the manifests in the index digest names, from the cache when
// it has them. The content behind a digest never changes, so a listing is
// reused for the TTL like any other answer.
func (c *Checker) Index(ctx context.Context, ref Ref, digest string) Result {
	now, ttl := c.now(), c.ttl()
	cache := c.loaded()
	key := ref.Name() + "@" + digest

	c.mu.Lock()
	hit, ok := cache.Entries[key]
	c.mu.Unlock()
	if ok && usable(hit, now, ttl) && (hit.Outcome != OutcomeDigest || hit.Listed) {
		hit.Cached = true
		return hit
	}

	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	res := c.client().Index(ctx, ref, digest)
	res.CheckedAt = now
	if cacheable(res.Outcome) {
		c.mu.Lock()
		cache.Entries[key] = res
		c.mu.Unlock()
		c.save()
	}
	return res
}

// Confirm records that ref's cached digest was found, fresh, to disagree with
// the running digests given. Until the entry expires, a later run that sees
// the same disagreement with the same running image answers from the cache
// instead of asking again; without this a container that is behind would be
// asked about on every run.
func (c *Checker) Confirm(ref Ref, running []string) {
	cache := c.loaded()
	key := ref.String()
	c.mu.Lock()
	e, ok := cache.Entries[key]
	changed := false
	if ok && e.Outcome == OutcomeDigest {
		for _, d := range running {
			if !contains(e.Confirmed, d) {
				e.Confirmed = append(e.Confirmed, d)
				changed = true
			}
		}
		cache.Entries[key] = e
	}
	c.mu.Unlock()
	if changed {
		c.save()
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// SaveError is why the cache could not be written, if it could not. A cache
// that cannot be written makes every run ask the registries again, which is
// worth saying once rather than finding out from a 429.
func (c *Checker) SaveError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveErr
}

func (c *Checker) loaded() *cacheFile {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		f := c.load()
		c.cache = &f
	}
	return c.cache
}

func (c *Checker) load() cacheFile {
	empty := cacheFile{Version: cacheVersion, Entries: map[string]Result{}}
	if c.CachePath == "" {
		return empty
	}
	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		return empty
	}
	var f cacheFile
	if json.Unmarshal(data, &f) != nil || f.Version != cacheVersion || f.Entries == nil {
		return empty
	}
	return f
}

// save writes the cache through a temporary file, so a doctor killed halfway
// leaves the previous cache rather than half of one. Entries past their TTL,
// or dated in the future, are dropped, so the file holds what is running and
// not every tag this machine has ever run.
func (c *Checker) save() {
	if c.CachePath == "" || c.ReadOnly {
		return
	}
	now, ttl := c.now(), c.ttl()
	c.mu.Lock()
	defer c.mu.Unlock()
	keep := cacheFile{Version: cacheVersion, Entries: map[string]Result{}}
	for key, e := range c.cache.Entries {
		if usable(e, now, ttl) {
			e.Cached = false
			keep.Entries[key] = e
		}
	}
	data, err := json.MarshalIndent(keep, "", "  ")
	if err != nil {
		c.saveErr = err
		return
	}
	dir := filepath.Dir(c.CachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.saveErr = err
		return
	}
	tmp, err := os.CreateTemp(dir, ".digests-*.json")
	if err != nil {
		c.saveErr = err
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), c.CachePath)
	}
	if werr == nil {
		c.saveErr = nil
		return
	}
	_ = os.Remove(tmp.Name())
	c.saveErr = fmt.Errorf("writing %s: %w", c.CachePath, werr)
}
