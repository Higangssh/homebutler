package util

import (
	"os"
	"sync"
)

var (
	dockerHostOnce sync.Once
)

// EnsureDockerHost detects the docker socket and sets DOCKER_HOST if needed.
// Call this once before any docker command. Safe to call multiple times.
func EnsureDockerHost() {
	dockerHostOnce.Do(func() {
		// Already set by user — respect it
		if os.Getenv("DOCKER_HOST") != "" {
			return
		}

		// Default socket works
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			return
		}

		// Try colima (macOS)
		home, _ := os.UserHomeDir()
		colimaSock := home + "/.colima/default/docker.sock"
		if _, err := os.Stat(colimaSock); err == nil {
			os.Setenv("DOCKER_HOST", "unix://"+colimaSock)
			return
		}

		// Try podman
		uid := os.Getuid()
		podmanSock := "/run/user/" + itoa(uid) + "/podman/podman.sock"
		if _, err := os.Stat(podmanSock); err == nil {
			os.Setenv("DOCKER_HOST", "unix://"+podmanSock)
			return
		}
	})
}

// DockerCmd runs a docker command with proper socket detection.
func DockerCmd(args ...string) (string, error) {
	EnsureDockerHost()
	return RunCmd("docker", args...)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for i > 0 {
		buf = append(buf, byte('0'+i%10))
		i /= 10
	}
	// reverse
	for l, r := 0, len(buf)-1; l < r; l, r = l+1, r-1 {
		buf[l], buf[r] = buf[r], buf[l]
	}
	return string(buf)
}
