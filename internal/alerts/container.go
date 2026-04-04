package alerts

import (
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/docker"
)

// ContainerStatus represents the state of a watched container.
type ContainerStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	State   string `json:"state"`
}

// CheckContainers checks whether the specified containers are running.
// Returns a list of statuses for each watched container.
func CheckContainers(names []string) ([]ContainerStatus, error) {
	containers, err := docker.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	lookup := make(map[string]docker.Container, len(containers))
	for _, c := range containers {
		lookup[c.Name] = c
	}

	results := make([]ContainerStatus, 0, len(names))
	for _, name := range names {
		c, found := lookup[name]
		if !found {
			results = append(results, ContainerStatus{
				Name:    name,
				Running: false,
				State:   "not found",
			})
			continue
		}
		state := strings.ToLower(c.State)
		results = append(results, ContainerStatus{
			Name:    name,
			Running: state == "running",
			State:   state,
		})
	}
	return results, nil
}
