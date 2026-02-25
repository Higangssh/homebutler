package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	Name         string
	Status       *system.StatusInfo
	Containers   []docker.Container
	DockerStatus string // "ok", "not_installed", "unavailable", ""
	Alerts       *alerts.AlertResult
	Error        error
	LastUpdate   time.Time
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

// fetchLocal gathers system status and alerts locally.
// Docker is skipped here and fetched separately to avoid blocking.
func fetchLocal(alertCfg *config.AlertConfig) ServerData {
	data := ServerData{LastUpdate: time.Now()}

	status, err := system.Status()
	if err != nil {
		data.Error = err
		return data
	}
	data.Status = status
	data.Name = status.Hostname

	alertResult, _ := alerts.Check(alertCfg)
	data.Alerts = alertResult

	return data
}

// fetchDocker fetches docker containers with a timeout.
func fetchDocker() ([]docker.Container, string) {
	type dockerResult struct {
		containers []docker.Container
		err        error
	}
	ch := make(chan dockerResult, 1)
	go func() {
		c, err := docker.List()
		ch <- dockerResult{c, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			errMsg := res.err.Error()
			if strings.Contains(errMsg, "not installed") || strings.Contains(errMsg, "not found") {
				return nil, "not_installed"
			}
			return nil, "unavailable"
		}
		return res.containers, "ok"
	case <-time.After(2 * time.Second):
		return nil, "unavailable"
	}
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
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not installed") || strings.Contains(errMsg, "not found") {
			data.DockerStatus = "not_installed"
		} else {
			data.DockerStatus = "unavailable"
		}
	} else {
		var containers []docker.Container
		if json.Unmarshal(out, &containers) == nil {
			data.DockerStatus = "ok"
			data.Containers = containers
		} else {
			data.DockerStatus = "unavailable"
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
