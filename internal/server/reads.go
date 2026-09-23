package server

import (
	"context"
	"net/http"

	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/install"
	"github.com/Higangssh/homebutler/internal/proxmox"
)

// The reads an action needs in order to be callable at all.
//
// Each of these names something another route then acts on: an archive to
// restore, an app to purge, a guest to shut down. They were absent while the
// actions beside them were not, which left the HTTP surface unable to answer
// its own question — the dashboard knew a container name because it had asked
// for one, and any other caller had nowhere to ask. See
// capability.HTTP.Target.

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	entries, err := backup.List(s.config().ResolveBackupDir())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, entries)
}

func (s *Server) handleInstallList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, install.List())
}

// handleInstallStatus answers for one app.
//
// Three answers, and they are three: a name that is not in the catalogue is a
// 404, an app in the catalogue that has never been installed is
// installed:false, and anything else is a failure. Not installed is the
// ordinary state of almost every entry — this route answered 500 for it until
// a screen asked.
func (s *Server) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("app")
	if _, ok := install.Registry[name]; !ok {
		writeError(w, http.StatusNotFound, "unknown app "+name)
		return
	}
	if !install.Installed(name) {
		writeJSON(w, map[string]any{"app": name, "installed": false})
		return
	}
	state, err := install.Status(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"app": name, "installed": true, "state": state})
}

// handleProxmoxGuests lists the guests on an endpoint, filtered the way the
// MCP tool filters them. The read credential, not the action one: listing is
// what a token that can only read is for.
func (s *Server) handleProxmoxGuests(w http.ResponseWriter, r *http.Request) {
	endpoint, err := s.config().SelectProxmox(r.URL.Query().Get("endpoint"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tokenID, token, err := endpoint.ResolveCredential(false)
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
	resources, err := client.Resources(context.Background())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	query := r.URL.Query()
	writeJSON(w, filterGuests(resources.Guests, query.Get("node"), query.Get("status"), query.Get("type")))
}

// filterGuests applies the same three filters the proxmox_guests tool applies,
// so the route and the tool cannot answer differently about the same endpoint.
func filterGuests(guests []proxmox.Guest, node, status, guestType string) []proxmox.Guest {
	out := make([]proxmox.Guest, 0, len(guests))
	for _, g := range guests {
		if node != "" && g.Node != node {
			continue
		}
		if status != "" && g.Status != status {
			continue
		}
		if guestType != "" && g.Type != guestType {
			continue
		}
		out = append(out, g)
	}
	return out
}
