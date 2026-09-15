package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/notify"
)

// saveMu serialises writes. Two browsers saving at once would otherwise both
// read the same revision, and the second would be refused as stale for a
// reason the person could do nothing about.
var saveMu sync.Mutex

// configSecret says whether a credential is set, and never what it is.
//
// The dashboard used to receive "••••••" for a password, which is a value
// shaped like one that can be sent back — and sending it back would write the
// bullets into the file. A boolean cannot be mistaken for a credential.
type configSecret struct {
	Set bool `json:"set"`
}

type channelSettings struct {
	Channel    string                  `json:"channel"`
	Configured bool                    `json:"configured"`
	Values     map[string]string       `json:"values,omitempty"`
	Secrets    map[string]configSecret `json:"secrets,omitempty"`
}

// notifySettings reports every channel homebutler has, whether or not it is
// configured, so the dashboard can offer one that has never been set up
// without carrying its own list.
func (s *Server) notifySettings() []channelSettings {
	out := make([]channelSettings, 0, len(notify.Channels()))
	enabled := map[notify.Channel]bool{}
	for _, c := range s.cfg.Notify.EnabledChannels() {
		enabled[c] = true
	}

	for _, channel := range notify.Channels() {
		fields, _ := notify.FieldsFor(channel)
		settings := channelSettings{
			Channel:    string(channel),
			Configured: enabled[channel],
			Values:     map[string]string{},
			Secrets:    map[string]configSecret{},
		}
		for _, f := range fields {
			value, set := s.cfg.Notify.Setting(channel, f.Name)
			if f.Secret {
				settings.Secrets[f.Name] = configSecret{Set: set && value != ""}
				continue
			}
			settings.Values[f.Name] = value
		}
		out = append(out, settings)
	}
	return out
}

type alertsRequest struct {
	Revision string   `json:"revision"`
	CPU      *float64 `json:"cpu,omitempty"`
	Memory   *float64 `json:"memory,omitempty"`
	Disk     *float64 `json:"disk,omitempty"`
}

// secretInput is how a credential arrives: not sent at all, sent with a new
// value, or explicitly cleared. An empty value is not "clear" — a form with an
// untouched box sends one, and taking that as a deletion loses a working
// setup because somebody changed a URL on the same page.
type secretInput struct {
	Value *string `json:"value,omitempty"`
	Clear bool    `json:"clear,omitempty"`
}

type notifyRequest struct {
	Revision string                        `json:"revision"`
	Channels map[string]notifyChannelInput `json:"channels"`
}

type notifyChannelInput struct {
	Values  map[string]string      `json:"values,omitempty"`
	Secrets map[string]secretInput `json:"secrets,omitempty"`
	Remove  bool                   `json:"remove,omitempty"`
}

// saveResponse tells the caller what to do next. Applied says whether the
// running processes have the change: serve reloads its own copy, and watch and
// alerts read the file when they start, so a change they care about waits for
// a restart. Saving and then appearing to do nothing is the state worth
// avoiding.
type saveResponse struct {
	Saved         bool     `json:"saved"`
	Revision      string   `json:"revision"`
	RestartNeeded []string `json:"restart_needed,omitempty"`
}

func (s *Server) handleSaveAlerts(w http.ResponseWriter, r *http.Request) {
	var req alertsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if req.CPU == nil && req.Memory == nil && req.Disk == nil {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}
	s.applySave(w, req.Revision, config.Patch{
		Alerts: &config.AlertsPatch{CPU: req.CPU, Memory: req.Memory, Disk: req.Disk},
	}, []string{"watch", "alerts"})
}

func (s *Server) handleSaveNotify(w http.ResponseWriter, r *http.Request) {
	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if len(req.Channels) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	patch := config.Patch{Notify: map[string]*config.NotifyPatch{}}
	for name, input := range req.Channels {
		channel := &config.NotifyPatch{Values: input.Values, Remove: input.Remove}
		if len(input.Secrets) > 0 {
			channel.Secrets = map[string]*config.Secret{}
			for key, secret := range input.Secrets {
				switch {
				case secret.Clear:
					channel.Secrets[key] = config.ClearSecret()
				case secret.Value != nil:
					channel.Secrets[key] = config.SetSecret(*secret.Value)
				}
			}
		}
		patch.Notify[name] = channel
	}

	s.applySave(w, req.Revision, patch, []string{"watch", "alerts"})
}

// applySave is the one place a write reaches the file, so the staleness check,
// the reload and the answer are decided once rather than per endpoint.
func (s *Server) applySave(w http.ResponseWriter, revision string, patch config.Patch, restart []string) {
	if s.cfg.Path == "" {
		writeError(w, http.StatusConflict, "there is no config file to write; run homebutler init first")
		return
	}

	saveMu.Lock()
	defer saveMu.Unlock()

	rev, err := config.ParseRevision(revision)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the request did not carry the revision it was made against")
		return
	}

	switch err := config.Save(s.cfg.Path, rev, patch); {
	case err == nil:
	case errors.Is(err, config.ErrStale):
		// 409 rather than 400: nothing about the request was wrong, and the
		// dashboard's answer is to reload rather than to change what it sent.
		writeError(w, http.StatusConflict, "the config file changed on disk since this page loaded it; reload before saving")
		return
	case errors.Is(err, config.ErrFlowStyle):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// serve owns the file it just wrote, so it reads it back rather than
	// carrying a copy that no longer matches.
	reloaded, err := config.Load(s.cfg.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but the file could not be read back")
		return
	}
	s.cfg = reloaded

	next, err := config.ReadRevision(s.cfg.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but the file could not be read back")
		return
	}

	writeJSON(w, saveResponse{Saved: true, Revision: next.String(), RestartNeeded: restart})
}

// transportWarning is shown on the settings screen when the token that unlocks
// it is travelling in the clear. It does not block: homebutler has no way to
// terminate TLS, and telling someone their setup is exposed is more use than
// refusing to work.
func (s *Server) transportWarning() string {
	if s.token == "" || !isPublicBind(s.host) {
		return ""
	}
	return "This dashboard is bound to " + s.host + " over plain HTTP, so the token unlocking it crosses the network in the clear. Put it behind a reverse proxy with TLS, or reach it over a tunnel."
}

func isPublicBind(host string) bool {
	h := strings.TrimSpace(host)
	return h != "" && h != "127.0.0.1" && h != "localhost" && h != "::1"
}
