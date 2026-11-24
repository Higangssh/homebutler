package docker

import (
	"reflect"
	"testing"
)

// The first two lines are what Docker 29 on the containerd image store
// printed on the test Pi for a pulled alpine and for an image built FROM it:
// both have RepoDigests, and only Identity tells them apart. The third is
// the classic store, which reports no Identity at all.
func TestParseImageOrigins(t *testing.T) {
	raw := `{"Id":"sha256:7772","RepoTags":["alpine:3.20"],"RepoDigests":["alpine@sha256:7772"],"Descriptor":{"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","digest":"sha256:7772"},"Identity":{"Pull":[{"Repository":"docker.io/library/alpine"}]}}
{"Id":"sha256:d614","RepoTags":["hb412-local:dev"],"RepoDigests":["hb412-local@sha256:d614"],"Identity":{"Build":[{"Ref":"nj8dde7s9icusrc0saih8pjxx","CreatedAt":"2026-11-24T23:32:50+09:00"}]}}
{"Id":"sha256:aaa1","RepoDigests":["nginx@sha256:1111","ghcr.io/x/nginx@sha256:2222"]}
{"Id":"sha256:bbb2","RepoDigests":null}
Error: No such image: sha256:ddd4
`
	got := parseImageOrigins(raw)
	want := map[string]ImageOrigin{
		"sha256:7772": {RepoDigests: []string{"alpine@sha256:7772"}, Known: true, PulledFrom: []string{"docker.io/library/alpine"}, MediaType: "application/vnd.docker.distribution.manifest.list.v2+json"},
		"sha256:d614": {RepoDigests: []string{"hb412-local@sha256:d614"}, Known: true, Built: true},
		"sha256:aaa1": {RepoDigests: []string{"nginx@sha256:1111", "ghcr.io/x/nginx@sha256:2222"}},
		"sha256:bbb2": {RepoDigests: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseImageOrigins =\n%#v\nwant\n%#v", got, want)
	}
}

// The reference is the third field; report's two-field reading of the same
// line has to keep working, since it is the same inspect.
func TestParseContainerImagesReadsTheReference(t *testing.T) {
	raw := "8246a831ee87a5942411827058f746f15a9a9bf1dab75363b415b213b3bbc720 sha256:7772 alpine@sha256:7772\n" +
		"c6d9b6730d12aa557e73833b4488464468f6771077e8bf7dc7fd5a7b8e884ba2 sha256:7b3c ghcr.io/containerd/busybox:1.36\n"
	got := parseContainerImages(raw)
	want := map[string]ContainerImage{
		"8246a831ee87": {ImageID: "sha256:7772", Reference: "alpine@sha256:7772"},
		"c6d9b6730d12": {ImageID: "sha256:7b3c", Reference: "ghcr.io/containerd/busybox:1.36"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseContainerImages = %#v, want %#v", got, want)
	}
	ids, err := imageIDsFrom([]string{"8246a831ee87", "c6d9b6730d12"}, raw, nil)
	if err != nil || ids["8246a831ee87"] != "sha256:7772" {
		t.Fatalf("imageIDsFrom = %v, %v", ids, err)
	}
}

// What docker info printed on the test Pi, and what the classic store's
// overlay2 prints; anything else is a store this cannot place.
func TestImageStoreFrom(t *testing.T) {
	for raw, want := range map[string]string{
		`[["driver-type","io.containerd.snapshotter.v1"]]`:                                       StoreContainerd,
		`[["Backing Filesystem","extfs"],["Supports d_type","true"],["Using metacopy","false"]]`: StoreClassic,
		`[["driver-type","something-else"]]`:                                                     "",
		`null`:                                                                                   "",
		`[]`:                                                                                     "",
		`not json`:                                                                               "",
	} {
		if got := imageStoreFrom(raw); got != want {
			t.Errorf("imageStoreFrom(%s) = %q, want %q", raw, got, want)
		}
	}
}
