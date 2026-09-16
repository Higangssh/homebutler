package system

import "os"

// InContainer reports whether this process is running inside a container.
//
// It matters because a container cannot see its host. Inside one, status reads
// the container's own /proc and ports sees its own sockets, so answering with
// those numbers would describe the wrong machine convincingly — which is worse
// than saying nothing.
//
// Docker writes /.dockerenv, Podman writes /run/.containerenv, and the env var
// is there for anything that does neither: an image built on top of this one,
// or a runtime nobody has thought of yet.
func InContainer() bool {
	if os.Getenv("HOMEBUTLER_CONTAINER") == "1" {
		return true
	}
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// ContainerCannotSeeHost is what to say instead of the container's own
// numbers. It names both ways out rather than only the problem: a reader who
// wanted their host watched has to end up doing one of them.
const ContainerCannotSeeHost = "homebutler is running in a container, which cannot see the machine it is on. " +
	"Watch that machine either by running homebutler on it directly, or by adding it to servers: and reaching it over SSH."
