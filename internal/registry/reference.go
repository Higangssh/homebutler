// Package registry asks a container registry which manifest a tag names now,
// so doctor can say when the registry has moved on and this machine has not.
//
// It sends anonymous requests only: a HEAD for each tag's manifest, the token
// request a registry's challenge asks for, and a GET of a multi-platform
// index when a running image disagrees with it and may be one of its
// children. Nothing is pulled into Docker, and no credential is read or sent.
package registry

import (
	"fmt"
	"strings"
)

// DockerHub is the registry a reference without a domain is pulled from.
const DockerHub = "docker.io"

// Ref is an image reference in the form Docker resolves it to.
type Ref struct {
	// Registry is the host the image is pulled from, with docker.io for
	// Docker Hub whichever spelling the reference used.
	Registry string
	// Repository is the path within the registry: library/nginx, not nginx.
	Repository string
	Tag        string
	// Digest is set when the reference is pinned, as in nginx@sha256:…
	Digest string
}

// Name is the repository with its registry, which is what a RepoDigests entry
// is compared against once both are normalised.
func (r Ref) Name() string { return r.Registry + "/" + r.Repository }

// String is the canonical reference, used as the cache key.
func (r Ref) String() string {
	s := r.Name()
	if r.Tag != "" {
		s += ":" + r.Tag
	}
	if r.Digest != "" {
		s += "@" + r.Digest
	}
	return s
}

// Pinned reports whether the reference names a digest, so there is nothing
// upstream for it to fall behind.
func (r Ref) Pinned() bool { return r.Digest != "" }

// ParseReference reads an image reference the way docker does: a first path
// component with a dot, a colon, or the word localhost is a registry, and
// anything else is Docker Hub, where a one-part name is an official image
// under library/.
func ParseReference(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ref{}, fmt.Errorf("empty image reference")
	}
	var ref Ref
	if name, digest, ok := strings.Cut(s, "@"); ok {
		if !strings.Contains(digest, ":") {
			return Ref{}, fmt.Errorf("%q has a malformed digest", s)
		}
		ref.Digest = digest
		s = name
	}

	// A tag follows the last colon after the last slash; a colon before it
	// is a registry port.
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		ref.Tag = s[i+1:]
		s = s[:i]
	}

	domain, rest, hasSlash := strings.Cut(s, "/")
	if hasSlash && (strings.ContainsAny(domain, ".:") || domain == "localhost") {
		ref.Registry = domain
		ref.Repository = rest
	} else {
		ref.Registry = DockerHub
		ref.Repository = s
	}
	if ref.Registry == "index.docker.io" || ref.Registry == "registry-1.docker.io" {
		ref.Registry = DockerHub
	}
	if ref.Registry == DockerHub && !strings.Contains(ref.Repository, "/") {
		ref.Repository = "library/" + ref.Repository
	}
	if ref.Repository == "" || strings.HasSuffix(ref.Repository, "/") || ref.Repository != strings.ToLower(ref.Repository) {
		return Ref{}, fmt.Errorf("%q is not an image reference", s)
	}
	if ref.Tag == "" && ref.Digest == "" {
		ref.Tag = "latest"
	}
	return ref, nil
}

// LooksLikeImageID reports whether docker ps printed an image ID where a
// reference would be. It does that when the tag the container was created
// from now names a different image on this machine.
func LooksLikeImageID(s string) bool {
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// RepoDigest splits a RepoDigests entry, such as nginx@sha256:…, into the
// normalised repository name and the digest.
func RepoDigest(entry string) (name, digest string, ok bool) {
	ref, err := ParseReference(entry)
	if err != nil || ref.Digest == "" {
		return "", "", false
	}
	return ref.Name(), ref.Digest, true
}
