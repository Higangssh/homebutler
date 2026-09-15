package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/notify"
)

// saveMu serialises writes. Two browsers saving at once would otherwise both
// read the same revision, and the second would be refused as stale for a
// reason the person could do nothing about.
var saveMu sync.Mutex

// maxSaveRequest caps what a save may send. The forms behind these endpoints
// change a handful of fields, so anything larger is a mistake or an attempt to
// make the process hold a body it was never going to use.
const maxSaveRequest = 1 << 20

// decodeSave reads a save request without letting the caller decide how much
// memory it costs.
func decodeSave(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxSaveRequest)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "that is larger than a settings change should ever be")
			return false
		}
		writeError(w, http.StatusBadRequest, "could not read the request")
		return false
	}
	return true
}

// newRevisionKey is made once per serve. It is what makes a revision token
// meaningless outside the process that issued it.
func newRevisionKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Carrying on would mean issuing tokens anyone could compute, which
		// is worse than refusing to start.
		panic("homebutler: could not read random bytes for the revision key: " + err.Error())
	}
	return key
}

// revisionToken is what the dashboard holds between loading a page and saving
// it. It is an HMAC of the file's digest rather than the digest itself, so the
// value handed to the browser says nothing about what is in the file and stops
// meaning anything once serve exits.
func (s *Server) revisionToken(rev config.Revision) string {
	if !rev.Exists() {
		return ""
	}
	mac := hmac.New(sha256.New, s.revisionKey)
	mac.Write([]byte(rev.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// matchRevision resolves the token a save carried back to the revision on disk
// now. The token never has to be reversed, because only equality is ever
// asked: if it still matches what the file hashes to, nothing changed
// underneath the page that sent it.
func (s *Server) matchRevision(path, token string) (config.Revision, error) {
	current, err := config.ReadRevision(path)
	if err != nil {
		return config.Revision{}, err
	}
	if !hmac.Equal([]byte(s.revisionToken(current)), []byte(token)) {
		return config.Revision{}, config.ErrStale
	}
	return current, nil
}

// channelField is one key of one channel as a form needs it: in the order the
// provider declares it, with a value when there is one to show and a flag when
// there is not.
type channelField struct {
	Name     string `json:"name"`
	Secret   bool   `json:"secret,omitempty"`
	Optional bool   `json:"optional,omitempty"`
	// Value is the field's contents, and is only ever populated for a field
	// that is not a credential.
	Value string `json:"value,omitempty"`
	// Set says whether a credential has one, and is nil for a field that is
	// not one. It is never the credential and never a stand-in shaped like
	// one, so the dashboard has nothing to send back by accident.
	Set *bool `json:"set,omitempty"`
}

type channelSettings struct {
	Channel    string         `json:"channel"`
	Configured bool           `json:"configured"`
	Fields     []channelField `json:"fields"`
}

// notifySettings reports every channel homebutler has, whether or not it is
// configured, so the dashboard can offer one that has never been set up
// without carrying its own list of channels or of the keys each one takes.
//
// The fields are a list rather than a map because a form is laid out in an
// order: ntfy asks for a server before a topic, and JSON objects do not keep
// the difference.
func (s *Server) notifySettings(cfg *config.Config) []channelSettings {
	out := make([]channelSettings, 0, len(notify.Channels()))
	enabled := map[notify.Channel]bool{}
	for _, c := range cfg.Notify.EnabledChannels() {
		enabled[c] = true
	}

	for _, channel := range notify.Channels() {
		fields, _ := notify.FieldsFor(channel)
		settings := channelSettings{
			Channel:    string(channel),
			Configured: enabled[channel],
			Fields:     make([]channelField, 0, len(fields)),
		}
		for _, f := range fields {
			value, present := cfg.Notify.Setting(channel, f.Name)
			field := channelField{Name: f.Name, Secret: f.Secret, Optional: f.Optional}
			if f.Secret {
				set := present && value != ""
				field.Set = &set
			} else {
				field.Value = value
			}
			settings.Fields = append(settings.Fields, field)
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
	if !decodeSave(w, r, &req) {
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
	if !decodeSave(w, r, &req) {
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
	path := s.config().Path
	if path == "" {
		writeError(w, http.StatusConflict, "there is no config file to write; run homebutler init first")
		return
	}
	if revision == "" {
		writeError(w, http.StatusBadRequest, "the request did not carry the revision it was made against")
		return
	}

	saveMu.Lock()
	defer saveMu.Unlock()

	rev, err := s.matchRevision(path, revision)
	switch {
	case err == nil:
	case errors.Is(err, config.ErrStale):
		// 409 rather than 400: nothing about the request was wrong, and the
		// dashboard's answer is to reload rather than to change what it sent.
		// A token from a previous serve lands here too, which is the same
		// answer — that page cannot know what the file holds now.
		writeError(w, http.StatusConflict, "the config file changed on disk since this page loaded it; reload before saving")
		return
	default:
		writeError(w, http.StatusInternalServerError, "the config file could not be read")
		return
	}

	switch err := config.Save(path, rev, patch); {
	case err == nil:
	case errors.Is(err, config.ErrStale):
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
	reloaded, err := config.Load(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but the file could not be read back")
		return
	}
	s.cfg.Store(reloaded)

	next, err := config.ReadRevision(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but the file could not be read back")
		return
	}

	writeJSON(w, saveResponse{Saved: true, Revision: s.revisionToken(next), RestartNeeded: restart})
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

// notifyTestResponse reports one result per configured channel. A channel that
// fails is a result rather than an error: the question the button asks is
// which channels work, and stopping at the first failure hides the rest.
type notifyTestResponse struct {
	Results []notify.TestResult `json:"results"`
}

// handleNotifyTest sends one real message. It is a write for the reason that
// matters — something leaves the machine and arrives on someone's phone — so
// it lives behind the token like every other write on this dashboard.
func (s *Server) handleNotifyTest(w http.ResponseWriter, _ *http.Request) {
	cfg := s.config()
	if len(cfg.Notify.EnabledChannels()) == 0 {
		writeError(w, http.StatusBadRequest, "no notification channel is configured yet")
		return
	}
	writeJSON(w, notifyTestResponse{Results: alerts.TestNotify(&cfg.Notify, alerts.TestEvent())})
}
