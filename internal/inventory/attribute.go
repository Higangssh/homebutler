package inventory

import (
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
)

// AttributePorts names the container behind each listener it can.
//
// On Linux a port published by Docker is held by docker-proxy, which runs as
// root, and `ss -tlnp` only reports the process for sockets the caller owns.
// Run as an ordinary user — which is how homebutler is meant to be run — every
// container port therefore looks anonymous, and on a homelab machine that is
// most of them. The mapping is in `docker ps`, collected in the same run, and
// reading it costs no privilege.
//
// It is done once, here, so that report, ports and inventory cannot disagree
// about who owns a port.
func AttributePorts(list []ports.PortInfo, containers []docker.Container) []ports.PortInfo {
	if len(list) == 0 || len(containers) == 0 {
		return list
	}
	for i := range list {
		if list[i].Container != "" {
			continue
		}
		if c, ok := docker.ContainerFor(containers, list[i].Port, list[i].Protocol); ok {
			list[i].Container = c.Describe()
		}
	}
	return list
}
