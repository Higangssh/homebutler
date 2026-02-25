package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
)

const fetchTimeout = 10 * time.Second

// ServerData holds all collected data for a single server.
type ServerData struct {
	Name       string
	Status     *system.StatusInfo
	Containers []docker.Container
	Alerts     *alerts.AlertResult
	Error      error
	LastUpdate time.Time
}

// fetchServer collects data from a server (local or remote) with a timeout.
func fetchServer(srv *config.ServerConfig, alertCfg *config.AlertConfig) ServerData {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	ch := make(chan ServerData, 1)
	go func() {
		if srv.Local {
			ch <- fetchLocal(alertCfg)
		} else {
			ch <- fetchRemote(srv, alertCfg)
		}
	}()

	select {
	case data := <-ch:
		return data
	case <-ctx.Done():
		return ServerData{
			Name:       srv.Name,
			Error:      fmt.Errorf("fetch timeout (%v)", fetchTimeout),
			LastUpdate: time.Now(),
		}
	}
}

// fetchLocal gathers system status, docker containers, and alerts locally.
// Docker and alerts are fetched with timeouts to avoid blocking the TUI.
func fetchLocal(alertCfg *config.AlertConfig) ServerData {
	data := ServerData{LastUpdate: time.Now()}

	status, err := system.Status()
	if err != nil {
		data.Error = err
		return data
	}
	data.Status = status
	data.Name = status.Hostname

	// Docker list with timeout (Docker Desktop may hang if not running)
	dockerCh := make(chan []docker.Container, 1)
	go func() {
		containers, _ := docker.List()
		dockerCh <- containers
	}()
	select {
	case containers := <-dockerCh:
		data.Containers = containers
	case <-time.After(3 * time.Second):
		data.Containers = nil // skip if Docker is unresponsive
	}

	alertResult, _ := alerts.Check(alertCfg)
	data.Alerts = alertResult

	return data
}

// fetchRemote collects data from a remote server via SSH.
func fetchRemote(srv *config.ServerConfig, alertCfg *config.AlertConfig) ServerData {
	data := ServerData{
		Name:       srv.Name,
		LastUpdate: time.Now(),
	}

	out, err := remote.Run(srv, "status", "--json")
	if err != nil {
		data.Error = err
		return data
	}
	var status system.StatusInfo
	if err := json.Unmarshal(out, &status); err != nil {
		data.Error = err
		return data
	}
	data.Status = &status

	// Docker containers (non-fatal)
	out, err = remote.Run(srv, "docker", "list", "--json")
	if err == nil {
		var containers []docker.Container
		if json.Unmarshal(out, &containers) == nil {
			data.Containers = containers
		}
	}

	// Alerts (non-fatal)
	out, err = remote.Run(srv, "alerts", "--json")
	if err == nil {
		var alertResult alerts.AlertResult
		if json.Unmarshal(out, &alertResult) == nil {
			data.Alerts = &alertResult
		}
	}

	return data
}
