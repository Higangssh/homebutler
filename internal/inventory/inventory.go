package inventory

import (
	"os"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
)

// Inventory holds the collected topology for a single server.
type Inventory struct {
	ServerName string             `json:"server_name"`
	Host       string             `json:"host"`
	System     *system.StatusInfo `json:"system"`
	Containers []docker.Container `json:"containers"`
	Ports      []ports.PortInfo   `json:"ports"`
	// Processes is collected so report can compare runs. It is not rendered
	// by inventory scan, which has its own process command.
	Processes []system.ProcessInfo `json:"-"`
	// ImageIDs is the image each container runs, keyed by container ID, for
	// the same reason and with the same reader as Processes.
	ImageIDs map[string]string `json:"-"`
	Warnings []string          `json:"warnings,omitempty"`
	// Failed names the collectors that did not answer, so a reader can ask
	// which part of the snapshot is missing rather than matching on the
	// prefix of a warning string. Warnings stays the human-readable list;
	// this is the machine-readable half, kept separate so the two do not have
	// to be one field doing both jobs.
	Failed []string `json:"failed_collectors,omitempty"`
}

// Collector names recorded in Failed.
const (
	CollectorDocker    = "docker"
	CollectorPorts     = "ports"
	CollectorProcesses = "processes"
)

// CollectorFailed reports whether the named collector failed during Collect.
// An empty Ports slice means either "nothing is listening" or "the scan did
// not run", and only this can tell them apart.
func (inv *Inventory) CollectorFailed(name string) bool {
	for _, f := range inv.Failed {
		if f == name {
			return true
		}
	}
	return false
}

// CollectFuncs allows injecting data sources for testing.
type CollectFuncs struct {
	StatusFn     func() (*system.StatusInfo, error)
	DockerListFn func() ([]docker.Container, error)
	PortsListFn  func() (*ports.Result, error)
	// ProcessesFn is optional and left unset by DefaultCollectFuncs. Only
	// report compares runs, and every other caller of Collect — inventory
	// scan, the MCP tools, doctor — would otherwise pay for a full ps sweep
	// whose result it never renders, and inherit a warning about it.
	ProcessesFn func() ([]system.ProcessInfo, error)
	// ImageIDsFn is optional for the same reason: only report compares which
	// image a container runs, and it costs a docker inspect.
	ImageIDsFn func(containerIDs []string) (map[string]string, error)
}

// DefaultCollectFuncs returns the real system/docker/ports functions.
func DefaultCollectFuncs() CollectFuncs {
	return CollectFuncs{
		StatusFn:     system.Status,
		DockerListFn: docker.List,
		PortsListFn:  ports.List,
	}
}

// Collect gathers inventory for the local server.
// Docker and ports failures are recorded as warnings, not errors.
func Collect(cfg *config.Config, fns CollectFuncs) (*Inventory, error) {
	inv := &Inventory{
		Containers: []docker.Container{},
		Ports:      []ports.PortInfo{},
	}

	// Determine server name and host from config.
	inv.ServerName, inv.Host = resolveServer(cfg)

	// System status is required.
	info, err := fns.StatusFn()
	if err != nil {
		return nil, err
	}
	inv.System = info

	// Docker: best-effort.
	containers, err := fns.DockerListFn()
	if err != nil {
		inv.Warnings = append(inv.Warnings, CollectorDocker+": "+err.Error())
		inv.Failed = append(inv.Failed, CollectorDocker)
	} else {
		inv.Containers = containers
		inv.ImageIDs = collectImageIDs(inv, fns.ImageIDsFn)
	}

	// Ports: best-effort.
	result, err := fns.PortsListFn()
	if err != nil {
		inv.Warnings = append(inv.Warnings, CollectorPorts+": "+err.Error())
		inv.Failed = append(inv.Failed, CollectorPorts)
	} else {
		// Attributed here, where both halves are in hand, so every consumer
		// of an inventory sees the same owner for a port.
		inv.Ports = AttributePorts(result.Ports, inv.Containers)
	}

	// Processes: best-effort, and optional so existing callers that build
	// CollectFuncs by hand keep working.
	if fns.ProcessesFn != nil {
		procs, err := fns.ProcessesFn()
		if err != nil {
			inv.Warnings = append(inv.Warnings, CollectorProcesses+": "+err.Error())
			inv.Failed = append(inv.Failed, CollectorProcesses)
		} else {
			inv.Processes = procs
		}
	}

	return inv, nil
}

// collectImageIDs asks which image each listed container runs. It is not a
// collector of its own and never lands in Failed: without it the containers
// are still compared, by the reference docker ps prints, as they were before
// image IDs were recorded. An error can come with a partial answer, which is
// kept: the containers it covers are compared by ID, and the warning names
// the ones that are not.
func collectImageIDs(inv *Inventory, fn func([]string) (map[string]string, error)) map[string]string {
	if fn == nil || len(inv.Containers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(inv.Containers))
	for _, c := range inv.Containers {
		if c.ID != "" {
			ids = append(ids, c.ID)
		}
	}
	got, err := fn(ids)
	if err != nil {
		inv.Warnings = append(inv.Warnings, CollectorDocker+": image IDs not read for every container, so those are compared by tag only: "+err.Error())
	}
	if len(got) == 0 {
		return nil
	}
	return got
}

// resolveServer picks the local server name/host from config,
// falling back to os.Hostname.
func resolveServer(cfg *config.Config) (name, host string) {
	if cfg != nil {
		for _, s := range cfg.Servers {
			if s.Local {
				return s.Name, s.Host
			}
		}
	}
	h, _ := os.Hostname()
	return h, h
}
