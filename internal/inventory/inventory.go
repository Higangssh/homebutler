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
	Warnings  []string             `json:"warnings,omitempty"`
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
	}

	// Ports: best-effort.
	result, err := fns.PortsListFn()
	if err != nil {
		inv.Warnings = append(inv.Warnings, CollectorPorts+": "+err.Error())
		inv.Failed = append(inv.Failed, CollectorPorts)
	} else {
		inv.Ports = result.Ports
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
