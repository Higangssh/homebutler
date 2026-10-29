package report

import (
	"strings"

	"github.com/Higangssh/homebutler/internal/system"
)

// A process that belongs to something the report already compares, or to
// homebutler itself, is left out of the process comparison.
//
// Recreating one container used to produce seven lines where two said
// anything: the container line, and five process lines saying it again. Each
// container's containerd-shim carries the container's ID in its arguments, so
// every recreation is a new invocation; the processes inside it come and go
// with it and change their command lines with its image. The container line
// already says that, with the image. Saying it again as processes the
// operator never started is the noise a report exists to leave out.
//
// The watch process's own helpers are left out for the same reason: an
// upgrade that changes the arguments of `watch start`'s `docker events` child
// is homebutler's doing, not something that happened to the machine. The
// watch process itself stays, since it starting or stopping is.
//
// Parentage is read from ps's ppid, which procps, BSD ps and busybox all
// have. cgroups would name the container directly, but macOS has no such
// column, and some processes show none on Linux.

// isContainerRuntime reports the per-container processes Docker runs on the
// host: one shim per container, and one proxy per published port. The port
// comparison already says when a published port changes.
func isContainerRuntime(name string) bool {
	return strings.HasPrefix(name, "containerd-shim") || name == "docker-proxy"
}

// isWatch reports the long-running `homebutler watch start` process.
func isWatch(p system.ProcessInfo) bool {
	if p.Name != "homebutler" {
		return false
	}
	args := strings.Fields(p.Command)
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "watch" && args[i+1] == "start" {
			return true
		}
	}
	return false
}

// maxAncestry bounds the walk up the parent chain. A pid table read in two ps
// calls is not a consistent snapshot, and a recycled pid could close a loop.
const maxAncestry = 64

// splitOwnedProcesses separates processes that belong to a container or to
// the watch process from the rest. owned is returned so that a comparison
// against a snapshot written before this split can leave the same processes
// out of the older side.
func splitOwnedProcesses(procs []system.ProcessInfo) (kept, owned []system.ProcessInfo) {
	byPID := make(map[int]system.ProcessInfo, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	ownedBy := func(p system.ProcessInfo) bool {
		if isContainerRuntime(p.Name) {
			return true
		}
		parent := p.PPID
		for range maxAncestry {
			a, ok := byPID[parent]
			if !ok || a.PID == p.PID {
				return false
			}
			if isContainerRuntime(a.Name) || isWatch(a) {
				return true
			}
			parent = a.PPID
		}
		return false
	}
	for _, p := range procs {
		if ownedBy(p) {
			owned = append(owned, p)
		} else {
			kept = append(kept, p)
		}
	}
	return kept, owned
}

// withoutOwned removes from an earlier snapshot's processes those this run
// leaves out as owned, so the first comparison after upgrading does not
// report every container's processes as gone.
//
// An identity is removed only when this run saw it owned and did not also see
// it outside a container: a host process with exactly the command line of a
// containerised one shares its identity, and dropping it from the older side
// would report it as new. Container runtime processes are removed by name,
// because their arguments carry an ID that changes with every container.
//
// A container recreated between the older snapshot and this run leaves its
// old processes unmatched, and they are reported gone once. Nothing in a
// snapshot written before this split can say whose they were.
func withoutOwned(prev []ProcessIdentity, owned, kept []system.ProcessInfo) []ProcessIdentity {
	ownedIDs := map[ProcessIdentity]bool{}
	for _, p := range owned {
		ownedIDs[identityOf(p)] = true
	}
	for _, p := range kept {
		delete(ownedIDs, identityOf(p))
	}
	out := make([]ProcessIdentity, 0, len(prev))
	for _, id := range prev {
		if isContainerRuntime(id.Name) || ownedIDs[id.key()] {
			continue
		}
		out = append(out, id)
	}
	return out
}
