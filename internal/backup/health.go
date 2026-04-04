package backup

import "time"

// HealthCheck defines how to verify that a restored app is working.
type HealthCheck struct {
	Path          string        // HTTP path to check (e.g. "/", "/health")
	ExpectCodes   []int         // acceptable HTTP status codes
	ContainerPort string        // container-side port to map
	BootTimeout   time.Duration // max wait for container to start
	HealthTimeout time.Duration // max wait for health endpoint to respond
}

const (
	DefaultBootTimeout   = 60 * time.Second
	DefaultHealthTimeout = 30 * time.Second
)

// HealthChecks maps app names to their health check configuration.
// Apps are based on the install.Registry definitions.
var HealthChecks = map[string]HealthCheck{
	"nginx-proxy-manager": {
		Path:          "/",
		ExpectCodes:   []int{200, 301},
		ContainerPort: "81",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"vaultwarden": {
		Path:          "/alive",
		ExpectCodes:   []int{200},
		ContainerPort: "80",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"uptime-kuma": {
		Path:          "/",
		ExpectCodes:   []int{200},
		ContainerPort: "3001",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"pi-hole": {
		Path:          "/admin",
		ExpectCodes:   []int{200, 301},
		ContainerPort: "80",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"gitea": {
		Path:          "/",
		ExpectCodes:   []int{200},
		ContainerPort: "3000",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"jellyfin": {
		Path:          "/health",
		ExpectCodes:   []int{200},
		ContainerPort: "8096",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"plex": {
		Path:          "/web",
		ExpectCodes:   []int{200, 301, 302},
		ContainerPort: "32400",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"portainer": {
		Path:          "/",
		ExpectCodes:   []int{200, 301, 302},
		ContainerPort: "9000",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"homepage": {
		Path:          "/",
		ExpectCodes:   []int{200},
		ContainerPort: "3000",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
	"adguard-home": {
		Path:          "/",
		ExpectCodes:   []int{200, 302},
		ContainerPort: "3000",
		BootTimeout:   DefaultBootTimeout,
		HealthTimeout: DefaultHealthTimeout,
	},
}
