package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/registry"
)

// ImageUpdateSource is what the image update check reads. Every field is
// injected so the check is testable without a docker daemon or a network.
type ImageUpdateSource struct {
	// ContainerImages is docker.ContainerImages: the image each container
	// runs and the reference it was created from, from the same inspect
	// report reads image IDs from.
	ContainerImages func(containerIDs []string) (map[string]docker.ContainerImage, error)
	// ImageOrigins is docker.ImageOrigins.
	ImageOrigins func(imageIDs []string) (map[string]docker.ImageOrigin, error)
	// ImageStore is docker.ImageStore. It is asked only when an image has no
	// Identity, which is the one case where the store decides what
	// RepoDigests can be trusted for.
	ImageStore func() (string, error)
	// NewLookup opens the registries for one run.
	NewLookup func(LookupConfig) RegistryLookup
}

// LookupConfig is what a run's registry lookups are set up with.
type LookupConfig struct {
	TTL time.Duration
	Now time.Time
	// NoSave reads the cache without writing it. The MCP server sets it in
	// read mode, and `doctor --no-save` sets it for a caller that has
	// promised to change nothing on disk.
	NoSave bool
}

// RegistryLookup is what the check asks of the registries. *registry.Checker
// is the real one.
type RegistryLookup interface {
	Check(ctx context.Context, refs []registry.Ref) map[string]registry.Result
	Recheck(ctx context.Context, refs []registry.Ref) map[string]registry.Result
	Index(ctx context.Context, ref registry.Ref, digest string) registry.Result
	// Confirm records that a fresh answer for ref disagreed with these
	// running digests, so the next run need not ask again to say so.
	Confirm(ref registry.Ref, running []string)
	SaveError() error
}

// imageCheckBudget bounds the whole check, every registry round included, so
// a machine with no route out pays this once.
const imageCheckBudget = 10 * time.Second

// DefaultImageUpdateSource reads docker and asks the real registries, caching
// their answers in ~/.homebutler/registry/digests.json.
func DefaultImageUpdateSource() *ImageUpdateSource {
	return &ImageUpdateSource{
		ContainerImages: docker.ContainerImages,
		ImageOrigins:    docker.ImageOrigins,
		ImageStore:      docker.ImageStore,
		NewLookup:       newRegistryLookup,
	}
}

func newRegistryLookup(cfg LookupConfig) RegistryLookup {
	return &registry.Checker{
		Client:    &registry.Client{UserAgent: "homebutler"},
		CachePath: defaultRegistryCachePath(),
		TTL:       cfg.TTL,
		Now:       cfg.Now,
		ReadOnly:  cfg.NoSave,
	}
}

func defaultRegistryCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".homebutler", "registry", "digests.json")
}

// imageCandidate is one running container the registry can be asked about.
type imageCandidate struct {
	name      string
	reference string
	ref       registry.Ref
	// pulled is the digests docker recorded for ref's repository when the
	// image was pulled. A registry digest in this list is what running the
	// current image looks like.
	pulled []string
	// retagged is true when docker ps printed an image ID for it: the tag
	// it was created from names another image on this machine now.
	retagged bool
	// pulledByIndex is true when docker says the image was pulled as a
	// multi-platform index. Its digest is then an index digest, and one
	// that is not the tag's is behind; there is no child to look for.
	pulledByIndex bool
}

// checkImageUpdates says when a running container's tag names a different
// image in its registry than the one the container runs.
//
// It says "different", not "newer": a tag moved back to an older image is
// as much a change, and nothing here can tell the two apart.
//
// What it says is info, never warn: an available update is not a fault, and
// a warn would turn `doctor --strict` red in cron every time an upstream tag
// moved, or every time this machine was offline. A container that matches is
// a pass.
//
// It compares the digest the registry answers for the tag with the digests
// docker recorded in RepoDigests on pull, not with the image ID: under the
// classic image store the ID is the digest of the image's config, which no
// registry manifest digest ever equals. For a multi-arch tag both sides are
// the digest of the index, so the comparison does not depend on the
// platform; an image pulled by one platform's digest is matched against the
// index's children.
//
// Nothing is reported from a cached answer alone. A cached digest that
// disagrees with what runs is asked again first, because the commonest
// reason for the disagreement is that the operator pulled the update since.
func checkImageUpdates(r *Result, cfg *config.Config, inv *inventory.Inventory, src *ImageUpdateSource, opts Options) {
	if src == nil || inv == nil {
		return
	}
	settings := config.ImageUpdatesConfig{}
	if cfg != nil {
		settings = cfg.Doctor.ImageUpdates
	}
	if !settings.On() {
		return
	}
	now := opts.Now

	var running []docker.Container
	for _, c := range inv.Containers {
		if c.State == "running" && c.ID != "" {
			running = append(running, c)
		}
	}
	if len(running) == 0 {
		return
	}
	sort.Slice(running, func(i, j int) bool { return running[i].Name < running[j].Name })

	var skipped []skippedImage
	skip := func(name, reference, reason string) {
		skipped = append(skipped, skippedImage{name, fmt.Sprintf("%s (%s): %s", name, reference, reason)})
	}

	ids := make([]string, 0, len(running))
	for _, c := range running {
		ids = append(ids, c.ID)
	}
	// A partial answer is used for what it covers; a container it left out
	// is skipped by name below.
	images, _ := src.ContainerImages(ids)

	type pending struct {
		name, reference string
		ref             registry.Ref
		imageID         string
		retagged        bool
	}
	var todo []pending
	for _, c := range running {
		img, ok := images[shortID(c.ID)]
		if !ok || img.ImageID == "" {
			skip(c.Name, c.Image, "docker did not say which image it runs")
			continue
		}
		reference := img.Reference
		if reference == "" {
			reference = c.Image
		}
		if registry.LooksLikeImageID(reference) {
			skip(c.Name, reference, "created from an image ID, so there is no tag to compare")
			continue
		}
		ref, err := registry.ParseReference(reference)
		if err != nil {
			skip(c.Name, reference, "the image reference could not be read")
			continue
		}
		if ref.Pinned() {
			skip(c.Name, reference, "pinned by digest, so there is no tag to fall behind")
			continue
		}
		todo = append(todo, pending{
			name: c.Name, reference: reference, ref: ref, imageID: img.ImageID,
			retagged: registry.LooksLikeImageID(c.Image),
		})
	}

	var origins map[string]docker.ImageOrigin
	var inspectErr error
	if len(todo) > 0 {
		var list []string
		for _, p := range todo {
			if !slices.Contains(list, p.imageID) {
				list = append(list, p.imageID)
			}
		}
		origins, inspectErr = src.ImageOrigins(list)
	}

	store := lazyStore(src.ImageStore)
	var candidates []imageCandidate
	var refs []registry.Ref
	for _, p := range todo {
		if inspectErr != nil {
			skip(p.name, p.reference, "docker did not say where its image came from: "+inspectErr.Error())
			continue
		}
		origin, ok := origins[p.imageID]
		if !ok {
			skip(p.name, p.reference, "docker did not say where its image came from")
			continue
		}
		pulled, reason := pulledDigests(origin, p.ref, store)
		if reason != "" {
			skip(p.name, p.reference, reason)
			continue
		}
		candidates = append(candidates, imageCandidate{name: p.name, reference: p.reference, ref: p.ref, pulled: pulled, retagged: p.retagged,
			pulledByIndex: origin.MediaType == registry.MediaOCIIndex || origin.MediaType == registry.MediaDockerList})
		refs = append(refs, p.ref)
	}

	if len(refs) == 0 {
		addImageSummary(r, nil, time.Time{}, now, skipped)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), imageCheckBudget)
	defer cancel()
	lookup := src.NewLookup(LookupConfig{TTL: settings.TTL(), Now: now, NoSave: opts.NoSave})
	results := lookup.Check(ctx, refs)

	// A cached answer that disagrees is asked again before it is used,
	// unless a fresh answer already disagreed with this same running image:
	// a container that is behind is otherwise asked about on every run.
	var stale []registry.Ref
	for _, c := range candidates {
		res, ok := results[c.ref.String()]
		if ok && res.Outcome == registry.OutcomeDigest && res.Cached && !slices.Contains(c.pulled, res.Digest) && !confirmedFor(res, c.pulled) {
			stale = append(stale, c.ref)
		}
	}
	if len(stale) > 0 {
		fresh := lookup.Recheck(ctx, stale)
		for _, ref := range stale {
			key := ref.String()
			if res, ok := fresh[key]; ok && res.Outcome == registry.OutcomeDigest {
				results[key] = res
				continue
			}
			reason := "the registry could not be asked again"
			if res, ok := fresh[key]; ok {
				reason = skipReason(res)
			}
			results[key] = registry.Result{Outcome: registry.OutcomeError,
				Reason: "the cached answer disagrees with what runs, and it was not reported without asking again: " + reason}
		}
	}

	var current []imageCandidate
	var oldest time.Time
	for _, c := range candidates {
		res, ok := results[c.ref.String()]
		if !ok {
			skip(c.name, c.reference, "the registry was not asked")
			continue
		}
		if res.Outcome != registry.OutcomeDigest {
			skip(c.name, c.reference, skipReason(res))
			continue
		}
		matched := slices.Contains(c.pulled, res.Digest)
		if !matched && res.IsIndex() && !c.pulledByIndex {
			// Perhaps pulled by one platform's digest and tagged: the
			// running digest would then be a child of the index, not the
			// index itself. Docker says which it was only from its
			// containerd store; elsewhere the index is fetched, and kept.
			idx := lookup.Index(ctx, c.ref, res.Digest)
			if idx.Outcome != registry.OutcomeDigest {
				skip(c.name, c.reference, "the tag names a multi-platform index whose list could not be read to compare against: "+skipReason(idx))
				continue
			}
			for _, d := range c.pulled {
				if slices.Contains(idx.Children, d) {
					matched = true
				}
			}
		}
		if oldest.IsZero() || res.CheckedAt.Before(oldest) {
			oldest = res.CheckedAt
		}
		if matched {
			current = append(current, c)
			continue
		}
		lookup.Confirm(c.ref, c.pulled)
		detail := fmt.Sprintf("On %s, %s names %s; this container runs the image pulled as %s.",
			c.ref.Registry, c.reference, shortDigest(res.Digest), shortDigest(c.pulled[0]))
		if c.retagged {
			detail += " The tag already names a different image on this machine, so it may have been pulled and the container not recreated."
		}
		r.add(SeverityInfo, "docker",
			fmt.Sprintf("%s: %s now names a different image in the registry", c.name, c.reference),
			detail+" "+checkedAgo(res.CheckedAt, now),
			"Pull and recreate it when you choose to. doctor does not pull or restart anything.", "")
	}

	addImageSummary(r, current, oldest, now, skipped)
	if err := lookup.SaveError(); err != nil {
		r.add(SeverityInfo, "docker", "Registry answers could not be saved",
			err.Error()+". Until this is fixed every doctor run asks the registries again, which spends Docker Hub's anonymous limit.",
			"Make ~/.homebutler/registry writable by this user.", "")
	}
}

// confirmedFor reports whether res was found, fresh, to disagree with one of
// the digests this container runs.
func confirmedFor(res registry.Result, pulled []string) bool {
	for _, d := range pulled {
		if slices.Contains(res.Confirmed, d) {
			return true
		}
	}
	return false
}

// skippedImage is one line of the not-checked finding.
type skippedImage struct{ name, line string }

func addImageSummary(r *Result, current []imageCandidate, oldest, now time.Time, skipped []skippedImage) {
	if len(current) > 0 {
		names := make([]string, 0, len(current))
		for _, c := range current {
			names = append(names, c.name)
		}
		r.add(SeverityPass, "docker",
			fmt.Sprintf("%d running container(s) match what their registry serves", len(current)),
			strings.Join(names, ", ")+". "+checkedAgo(oldest, now), "", "")
	}
	if len(skipped) > 0 {
		sort.SliceStable(skipped, func(i, j int) bool { return skipped[i].name < skipped[j].name })
		lines := make([]string, 0, len(skipped))
		for _, s := range skipped {
			lines = append(lines, s.line)
		}
		r.add(SeverityInfo, "docker",
			fmt.Sprintf("%d running container(s) not checked against their registry", len(skipped)),
			strings.Join(lines, "; "),
			"Nothing to fix unless one of these should be checked. Set doctor.image_updates.enabled to false to stop doctor asking registries at all.", "")
	}
}

// lazyStore asks docker which image store it uses at most once, and only if
// something needs to know.
func lazyStore(fn func() (string, error)) func() string {
	asked, store := false, ""
	return func() string {
		if !asked {
			asked = true
			if fn != nil {
				store, _ = fn()
			}
		}
		return store
	}
}

// pulledDigests returns the digests docker recorded for ref's repository, or
// why there is nothing to compare.
//
// Where docker says how an image arrived (Identity, Docker 29.2 and API 1.53
// on), that decides. Where it does not, RepoDigests is trusted only under
// the classic image store, which records one only on pull. The containerd
// store gives a locally built image a RepoDigests entry under its own name,
// and taking that as a pull would send the name to Docker Hub and report
// whatever came back about it; so under that store, or a store this cannot
// name, nothing is asked.
func pulledDigests(origin docker.ImageOrigin, ref registry.Ref, store func() string) ([]string, string) {
	if origin.Known {
		pulledHere := false
		for _, repo := range origin.PulledFrom {
			if r, err := registry.ParseReference(repo); err == nil && r.Name() == ref.Name() {
				pulledHere = true
			}
		}
		if !pulledHere {
			if len(origin.PulledFrom) == 0 {
				return nil, "built on this machine or loaded from a file, so there is no registry digest to compare"
			}
			return nil, "its image was pulled under another name, so nothing records what " + ref.Name() + " was when it was pulled"
		}
	} else {
		switch store() {
		case docker.StoreClassic:
			if len(origin.RepoDigests) == 0 {
				return nil, "built on this machine or loaded from a file, so there is no registry digest to compare"
			}
		case docker.StoreContainerd:
			return nil, "Docker before 29.2 (API 1.53) does not say whether an image was pulled or built here, and under the containerd image store both have registry digests, so the registry was not asked"
		default:
			return nil, "could not tell which image store docker uses, so whether the image was pulled or built here is unknown and the registry was not asked"
		}
	}

	var pulled []string
	for _, entry := range origin.RepoDigests {
		if name, digest, ok := registry.RepoDigest(entry); ok && name == ref.Name() {
			pulled = append(pulled, digest)
		}
	}
	if len(pulled) == 0 {
		return nil, "its image was pulled under another name, so nothing records what " + ref.Name() + " was when it was pulled"
	}
	return pulled, ""
}

func skipReason(res registry.Result) string {
	switch res.Outcome {
	case registry.OutcomeAuth:
		return "the registry would not answer without credentials, and this check only asks anonymously"
	case registry.OutcomeRateLimited:
		return "the registry is rate limiting this address; that answer is kept until the cache expires, so the limit is not spent asking again"
	case registry.OutcomeNotFound:
		return res.Reason
	case registry.OutcomeUnreachable:
		return "could not reach the registry: " + res.Reason
	default:
		if res.Reason != "" {
			return res.Reason
		}
		return "the registry's answer could not be used"
	}
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// shortDigest keeps the algorithm and twelve hex digits, which is as much as
// `docker images --digests` makes anyone compare by eye.
func shortDigest(d string) string {
	algo, hex, ok := strings.Cut(d, ":")
	if !ok || len(hex) <= 12 {
		return d
	}
	return algo + ":" + hex[:12]
}

// checkedAgo says how old the registry's answer is. An answer asked for this
// run carries the run's own clock, so only that one is "just now"; a cached
// answer from seconds ago is said to be from the cache.
func checkedAgo(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	age := now.Sub(at)
	switch {
	case age <= 0:
		return "Asked the registry just now."
	case age < time.Minute:
		return "Registry answer from under a minute ago."
	case age < time.Hour:
		return fmt.Sprintf("Registry answer from %dm ago.", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("Registry answer from %dh ago.", int(age.Hours()))
	}
	return "Registry answer from " + roundDuration(age) + " ago."
}
