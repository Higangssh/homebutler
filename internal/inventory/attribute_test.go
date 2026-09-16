package inventory

import (
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
)

// The shape Linux produces for a port published by Docker, run as an ordinary
// user: `ss -tlnp` prints no users:(...) column at all, because the socket is
// held by root's docker-proxy. Every test written against macOS output misses
// this, which is how it reached a Raspberry Pi.
func linuxDockerPort() ports.PortInfo {
	return ports.PortInfo{Protocol: "tcp", Address: "0.0.0.0", Port: "18083"} // no PID, no Process
}

func TestAContainerPortIsNamedWhenTheSystemCannotNameIt(t *testing.T) {
	containers := []docker.Container{
		{Name: "pm-probe-c", Image: "traefik:v3.1", State: "running", Ports: "0.0.0.0:18083->80/tcp, [::]:18083->80/tcp"},
	}

	got := AttributePorts([]ports.PortInfo{linuxDockerPort()}, containers)
	if got[0].Container != "pm-probe-c (traefik:v3.1)" {
		t.Fatalf("the container behind the port was not named: %q", got[0].Container)
	}
	// The process field stays empty: that is what the operating system said,
	// and overwriting it would make a guess look like a reading.
	if got[0].Process != "" {
		t.Fatalf("the process field was invented: %q", got[0].Process)
	}
}

// A port no container claims is left alone. Saying a container answers it
// would be worse than saying nothing.
func TestAPortWithNoContainerIsLeftAlone(t *testing.T) {
	containers := []docker.Container{
		{Name: "other", Image: "nginx:alpine", Ports: "0.0.0.0:8080->80/tcp"},
	}
	got := AttributePorts([]ports.PortInfo{linuxDockerPort()}, containers)
	if got[0].Container != "" {
		t.Fatalf("a port nothing publishes was attributed to %q", got[0].Container)
	}
}

// What the system did say is not overwritten by a guess.
func TestAProcessTheSystemNamedIsKept(t *testing.T) {
	named := linuxDockerPort()
	named.Process = "nginx"
	containers := []docker.Container{{Name: "pm-probe-c", Image: "traefik:v3.1", Ports: "0.0.0.0:18083->80/tcp"}}

	got := AttributePorts([]ports.PortInfo{named}, containers)
	if got[0].Process != "nginx" {
		t.Fatalf("the reported process changed to %q", got[0].Process)
	}
}

func TestPublishedPortsReadsWhatDockerPrints(t *testing.T) {
	cases := []struct {
		name  string
		ports string
		want  []docker.PublishedPort
	}{
		{"both families", "0.0.0.0:18079->80/tcp, [::]:18079->80/tcp",
			[]docker.PublishedPort{{Port: "18079", Protocol: "tcp"}, {Port: "18079", Protocol: "tcp"}}},
		{"loopback only", "127.0.0.1:8080->80/tcp",
			[]docker.PublishedPort{{Port: "8080", Protocol: "tcp"}}},
		{"udp", "0.0.0.0:53->53/udp",
			[]docker.PublishedPort{{Port: "53", Protocol: "udp"}}},
		// Exposed by the image and published by nobody: nothing is listening
		// on the host, so there is no host port to attribute.
		{"exposed only", "80/tcp", nil},
		{"a range", "0.0.0.0:3000-3002->3000-3002/tcp",
			[]docker.PublishedPort{{Port: "3000", Protocol: "tcp"}, {Port: "3001", Protocol: "tcp"}, {Port: "3002", Protocol: "tcp"}}},
		{"nothing", "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := docker.Container{Ports: tc.ports}.PublishedPorts()
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
