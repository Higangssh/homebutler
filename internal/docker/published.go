package docker

import (
	"strconv"
	"strings"
)

// PublishedPort is one host port a container answers on.
type PublishedPort struct {
	Port     string
	Protocol string
}

// PublishedPorts reads the mapping docker ps prints, which looks like
//
//	0.0.0.0:8080->80/tcp, [::]:8080->80/tcp
//
// An entry with no arrow is a port the image exposes and nothing publishes, so
// nothing is listening on the host for it.
func (c Container) PublishedPorts() []PublishedPort {
	var out []PublishedPort
	for _, mapping := range strings.Split(c.Ports, ",") {
		mapping = strings.TrimSpace(mapping)
		arrow := strings.Index(mapping, "->")
		if arrow < 0 {
			continue
		}

		host := mapping[:arrow]
		protocol := "tcp"
		if slash := strings.LastIndex(mapping, "/"); slash > arrow {
			protocol = mapping[slash+1:]
		}

		// The host side is address:port, and the address may be an IPv6
		// literal in brackets, so the port is whatever follows the last colon.
		colon := strings.LastIndex(host, ":")
		if colon < 0 {
			continue
		}
		for _, port := range expandRange(host[colon+1:]) {
			out = append(out, PublishedPort{Port: port, Protocol: protocol})
		}
	}
	return out
}

// expandRange turns "3000-3002" into the ports it covers. Docker prints a
// range when one was published as a range, and a container that answers on
// 3001 should be found by looking up 3001.
func expandRange(spec string) []string {
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return []string{spec}
	}
	first, err1 := strconv.Atoi(spec[:dash])
	last, err2 := strconv.Atoi(spec[dash+1:])
	if err1 != nil || err2 != nil || last < first || last-first > 1024 {
		return []string{spec}
	}
	out := make([]string, 0, last-first+1)
	for port := first; port <= last; port++ {
		out = append(out, strconv.Itoa(port))
	}
	return out
}

// ContainerFor returns the container publishing a host port, if one does.
//
// It exists because the process holding a published port is docker-proxy,
// which runs as root: `ss -tlnp` shows no owner for it unless the caller is
// root, so on Linux as an ordinary user every container port looks anonymous.
// The answer was collected in the same run — docker ps prints the mapping —
// and asking for it needs no privilege at all.
func ContainerFor(containers []Container, port, protocol string) (Container, bool) {
	if protocol == "" {
		protocol = "tcp"
	}
	for _, c := range containers {
		for _, published := range c.PublishedPorts() {
			if published.Port == port && published.Protocol == protocol {
				return c, true
			}
		}
	}
	return Container{}, false
}

// Describe names a container the way somebody would recognise it.
func (c Container) Describe() string {
	if c.Image == "" {
		return c.Name
	}
	return c.Name + " (" + c.Image + ")"
}
