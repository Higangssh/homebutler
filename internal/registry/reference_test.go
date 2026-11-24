package registry

import "testing"

func TestParseReference(t *testing.T) {
	cases := []struct {
		in                       string
		registry, repo, tag, dig string
	}{
		{"nginx", "docker.io", "library/nginx", "latest", ""},
		{"nginx:1.27", "docker.io", "library/nginx", "1.27", ""},
		{"linuxserver/plex:latest", "docker.io", "linuxserver/plex", "latest", ""},
		{"docker.io/library/alpine:3.20", "docker.io", "library/alpine", "3.20", ""},
		{"index.docker.io/library/alpine", "docker.io", "library/alpine", "latest", ""},
		{"registry-1.docker.io/library/alpine:3", "docker.io", "library/alpine", "3", ""},
		{"registry-1.docker.io/nginx", "docker.io", "library/nginx", "latest", ""},
		{"ghcr.io/home-assistant/home-assistant:stable", "ghcr.io", "home-assistant/home-assistant", "stable", ""},
		{"localhost:5000/app", "localhost:5000", "app", "latest", ""},
		{"registry.lan:5000/team/app:v2", "registry.lan:5000", "team/app", "v2", ""},
		{"localhost/app:dev", "localhost", "app", "dev", ""},
		{"alpine@sha256:abc", "docker.io", "library/alpine", "", "sha256:abc"},
		{"alpine:3.20@sha256:abc", "docker.io", "library/alpine", "3.20", "sha256:abc"},
	}
	for _, c := range cases {
		got, err := ParseReference(c.in)
		if err != nil {
			t.Errorf("ParseReference(%q): %v", c.in, err)
			continue
		}
		if got.Registry != c.registry || got.Repository != c.repo || got.Tag != c.tag || got.Digest != c.dig {
			t.Errorf("ParseReference(%q) = %+v, want %s %s %s %s", c.in, got, c.registry, c.repo, c.tag, c.dig)
		}
	}
}

func TestParseReferenceRejects(t *testing.T) {
	for _, in := range []string{"", "Nginx", "ghcr.io/", "alpine@nodigest"} {
		if ref, err := ParseReference(in); err == nil {
			t.Errorf("ParseReference(%q) = %+v, want an error", in, ref)
		}
	}
}

// A RepoDigests entry spells Docker Hub the short way, and a reference can
// spell it the long way. Both have to land on the same name, or every Hub
// image looks pulled under another name and is never checked.
func TestRepoDigestNormalisesLikeTheReference(t *testing.T) {
	ref, _ := ParseReference("docker.io/library/nginx:latest")
	name, digest, ok := RepoDigest("nginx@sha256:0123")
	if !ok || name != ref.Name() || digest != "sha256:0123" {
		t.Fatalf("RepoDigest = %q %q %v, want %q", name, digest, ok, ref.Name())
	}
	if _, _, ok := RepoDigest("nginx:latest"); ok {
		t.Fatal("an entry without a digest is not a RepoDigests entry")
	}
}

func TestLooksLikeImageID(t *testing.T) {
	for _, s := range []string{"a1b2c3d4e5f6", "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"} {
		if !LooksLikeImageID(s) {
			t.Errorf("%q is an image ID", s)
		}
	}
	for _, s := range []string{"nginx", "deadbeef", "nginx:latest", "a1b2c3d4e5fz"} {
		if LooksLikeImageID(s) {
			t.Errorf("%q is not an image ID", s)
		}
	}
}
