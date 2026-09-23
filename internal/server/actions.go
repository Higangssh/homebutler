package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/install"
	"github.com/Higangssh/homebutler/internal/proxmox"
	"github.com/Higangssh/homebutler/internal/watch"
)

// The actions the dashboard can run, as opposed to the settings it can save.
//
// What each one costs a caller is decided in internal/capability and applied
// once, where the routes are registered — a handler here does the thing and
// does not carry its own gate, because a gate per handler is a gate the next
// handler forgets.

func (s *Server) handleDockerRestart(w http.ResponseWriter, r *http.Request) {
	result, err := docker.Restart(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDockerStop(w http.ResponseWriter, r *http.Request) {
	result, err := docker.Stop(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	result, err := backup.Run(cfg.ResolveBackupDir(), bodyString(r, "service"), cfg.ResolveBackupRetention(), bodyStrings(r, "exclude"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleBackupDrill(w http.ResponseWriter, r *http.Request) {
	opts := backup.DrillOptions{
		BackupDir: s.config().ResolveBackupDir(),
		Archive:   bodyString(r, "archive"),
	}
	app := bodyString(r, "app")
	if app == "" {
		report, err := backup.RunDrillAll(opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, report)
		return
	}
	result, err := backup.RunDrill(app, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	// No AllowBind, for the same reason the MCP tool has none: nothing in a
	// browser can name a host path it is permitted to write to, so a bind
	// mount the archive declares is refused and reported.
	result, err := backup.Restore(bodyString(r, "archive"), backup.RestoreOptions{
		Service: bodyString(r, "service"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleInstallApp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("app")
	app, ok := install.Registry[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown app %q", name))
		return
	}
	port := bodyString(r, "port")
	if port == "" {
		port = app.DefaultPort
	}
	// The pre-flight is an answer, not an error: a refusal tells the operator
	// which port is taken so they can change it and ask again.
	if issues := install.PreCheck(app, port); len(issues) > 0 {
		writeJSON(w, installResponse{Status: "failed", App: app.Name, Port: port, Issues: issues})
		return
	}
	if err := install.Install(app, install.InstallOptions{Port: port}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state, _ := install.Status(app.Name)
	writeJSON(w, installResponse{
		Status: "installed", App: app.Name, Port: port,
		Path: install.AppDir(app.Name), State: state,
	})
}

func (s *Server) handleInstallUninstall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("app")
	if err := install.Uninstall(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	preserved := true
	writeJSON(w, installResponse{Status: "uninstalled", App: name, DataPreserved: &preserved})
}

func (s *Server) handleInstallPurge(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("app")
	if err := install.Purge(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gone := false
	writeJSON(w, installResponse{Status: "purged", App: name, DataPreserved: &gone})
}

// installResponse is the same shape the MCP tool answers with, named here so
// the two surfaces cannot drift into describing the same outcome differently.
type installResponse struct {
	Status        string   `json:"status"`
	App           string   `json:"app"`
	Issues        []string `json:"issues,omitempty"`
	Port          string   `json:"port,omitempty"`
	Path          string   `json:"path,omitempty"`
	State         string   `json:"state,omitempty"`
	DataPreserved *bool    `json:"data_preserved,omitempty"`
}

func (s *Server) handleWatchAdd(w http.ResponseWriter, r *http.Request) {
	container := bodyString(r, "container")
	if container == "" {
		writeError(w, http.StatusBadRequest, "container is required")
		return
	}
	kind := bodyString(r, "kind")
	if kind == "" {
		kind = watch.KindDocker
	}
	dir, err := watch.WatchDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	added, err := watch.AddTarget(dir, watch.Target{
		Container: container, Kind: kind, Unit: bodyString(r, "unit"),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"container": container, "kind": kind, "added": added})
}

func (s *Server) handleWatchRemove(w http.ResponseWriter, r *http.Request) {
	dir, err := watch.WatchDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := r.PathValue("name")
	removed, err := watch.RemoveTarget(dir, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s is not on the watch list", name))
		return
	}
	writeJSON(w, map[string]any{"container": name, "removed": true})
}

func (s *Server) handleWatchCheck(w http.ResponseWriter, r *http.Request) {
	dir, err := watch.WatchDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := watch.CheckTargets(dir, s.config().Watch.Retention.MaxIncidents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) proxmoxGuestAction(action proxmox.GuestAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoint, err := s.config().SelectProxmox(r.URL.Query().Get("endpoint"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// The action credential, not the read one: without it these are
		// unavailable rather than falling back to a token that can only read.
		tokenID, token, err := endpoint.ResolveCredential(true)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		client, err := proxmox.New(proxmox.Options{
			Host: endpoint.Host, Port: endpoint.APIPort(), TokenID: tokenID, Token: token,
			Fingerprint: endpoint.Fingerprint, CAFile: endpoint.CAFile,
			Insecure: endpoint.Insecure, Timeout: endpoint.TimeoutDuration(),
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		vmid, err := strconv.Atoi(r.PathValue("vmid"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "vmid must be a number")
			return
		}
		node, guestType := bodyString(r, "node"), bodyString(r, "type")
		if node == "" || guestType == "" {
			writeError(w, http.StatusBadRequest, "node and type are required: a guest is addressed explicitly, never guessed")
			return
		}
		upid, err := client.ActOnGuest(context.Background(), node, guestType, vmid, action)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"endpoint": endpoint.Name, "node": node, "type": guestType,
			"vmid": vmid, "action": string(action), "status": "accepted", "upid": upid,
		})
	}
}
