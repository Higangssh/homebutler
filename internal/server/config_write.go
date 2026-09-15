package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	// Notices are things that are true about what was just saved rather than
	// reasons it failed — a new address that will be trusted on first sight,
	// for instance. Saving and finding out later is the state worth avoiding.
	Notices []string `json:"notices,omitempty"`
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

type wakeRequest struct {
	Revision string       `json:"revision"`
	Targets  []wakeTarget `json:"targets"`
}

// wakeTarget is one machine as the dashboard sends it back. Nothing here is a
// credential — a MAC address is on the network already — so it travels in both
// directions, unlike a notification token.
type wakeTarget struct {
	Name      string `json:"name"`
	MAC       string `json:"mac,omitempty"`
	Broadcast string `json:"broadcast,omitempty"`
	Remove    bool   `json:"remove,omitempty"`
}

func (s *Server) handleSaveWake(w http.ResponseWriter, r *http.Request) {
	var req wakeRequest
	if !decodeSave(w, r, &req) {
		return
	}
	if len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	patch := config.Patch{Wake: make([]config.WakePatch, 0, len(req.Targets))}
	for _, target := range req.Targets {
		patch.Wake = append(patch.Wake, config.WakePatch{
			Name:      target.Name,
			MAC:       target.MAC,
			Broadcast: target.Broadcast,
			Remove:    target.Remove,
		})
	}

	// Nothing has to be restarted: serve reads the file back after it writes,
	// and the CLI reads it on every run. A wake target is usable the moment it
	// is saved.
	s.applySave(w, req.Revision, patch, nil)
}

// applySave is the one place a write reaches the file, so the staleness check,
// the reload and the answer are decided once rather than per endpoint.
// applySave reports whether the file was written, so a caller can do the
// things that are only true afterwards — writing a move to the log, for one.
func (s *Server) applySave(w http.ResponseWriter, revision string, patch config.Patch, restart []string, notices ...string) bool {
	path := s.config().Path
	if path == "" {
		writeError(w, http.StatusConflict, "there is no config file to write; run homebutler init first")
		return false
	}
	if revision == "" {
		writeError(w, http.StatusBadRequest, "the request did not carry the revision it was made against")
		return false
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
		return false
	default:
		writeError(w, http.StatusInternalServerError, "the config file could not be read")
		return false
	}

	switch err := config.Save(path, rev, patch); {
	case err == nil:
	case errors.Is(err, config.ErrStale):
		writeError(w, http.StatusConflict, "the config file changed on disk since this page loaded it; reload before saving")
		return false
	case errors.Is(err, config.ErrFlowStyle):
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	default:
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}

	// serve owns the file it just wrote, so it reads it back rather than
	// carrying a copy that no longer matches.
	reloaded, err := config.Load(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but the file could not be read back")
		return false
	}
	s.cfg.Store(reloaded)

	next, err := config.ReadRevision(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but the file could not be read back")
		return false
	}

	writeJSON(w, saveResponse{Saved: true, Revision: s.revisionToken(next), RestartNeeded: restart, Notices: notices})
	return true
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

type serversRequest struct {
	Revision string        `json:"revision"`
	Servers  []serverInput `json:"servers"`
}

// serverInput is one server as the dashboard sends it back. A field that is
// not sent is not changed, which is why everything optional is a pointer: an
// empty string is a value somebody typed, not an absence.
//
// There is no key file here on purpose. It names a path on the machine
// homebutler runs on, and choosing one from a browser is a different thing
// from choosing a password; the CLI still edits it.
type serverInput struct {
	Name     string       `json:"name"`
	Rename   string       `json:"rename,omitempty"`
	Host     *string      `json:"host,omitempty"`
	Port     *int         `json:"port,omitempty"`
	User     *string      `json:"user,omitempty"`
	Auth     *string      `json:"auth,omitempty"`
	Password *secretInput `json:"password,omitempty"`
	Remove   bool         `json:"remove,omitempty"`
}

type proxmoxRequest struct {
	Revision  string                 `json:"revision"`
	Endpoints []proxmoxEndpointInput `json:"endpoints"`
}

type proxmoxEndpointInput struct {
	Name          string       `json:"name"`
	Rename        string       `json:"rename,omitempty"`
	Host          *string      `json:"host,omitempty"`
	Port          *int         `json:"port,omitempty"`
	TokenID       *string      `json:"token_id,omitempty"`
	Token         *secretInput `json:"token,omitempty"`
	ActionTokenID *string      `json:"action_token_id,omitempty"`
	ActionToken   *secretInput `json:"action_token,omitempty"`
	Remove        bool         `json:"remove,omitempty"`
}

// secret turns what the form sent into the three states the writer knows:
// leave it alone, replace it, or remove it.
func (in *secretInput) secret() *config.Secret {
	switch {
	case in == nil:
		return nil
	case in.Clear:
		return config.ClearSecret()
	case in.Value != nil:
		return config.SetSecret(*in.Value)
	default:
		return nil
	}
}

func (s *Server) handleSaveServers(w http.ResponseWriter, r *http.Request) {
	var req serversRequest
	if !decodeSave(w, r, &req) {
		return
	}
	if len(req.Servers) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	cfg := s.config()
	patch := config.Patch{Servers: make([]config.ServerPatch, 0, len(req.Servers))}
	var notices, moves []string

	for _, in := range req.Servers {
		patch.Servers = append(patch.Servers, config.ServerPatch{
			Name:     in.Name,
			Rename:   in.Rename,
			Host:     in.Host,
			Port:     in.Port,
			User:     in.User,
			AuthMode: in.Auth,
			Password: in.Password.secret(),
			Remove:   in.Remove,
		})

		if move, notice := s.noteMovedServer(cfg, in); move != "" {
			moves = append(moves, move)
			if notice != "" {
				notices = append(notices, notice)
			}
		}
	}

	// watch connects to these machines; it reads the file when it starts.
	if !s.applySave(w, req.Revision, patch, []string{"watch"}, notices...) {
		return
	}

	// Logged after the write rather than before it: a refused save is not a
	// change, and a log that says otherwise is worse than no log at all.
	for _, move := range moves {
		log.Printf("config: %s", move)
	}
}

// noteMovedServer says out loud what changing an address means for a server
// that signs in with a key, and writes the move to the log.
//
// The credential rule in internal/config refuses to move a saved password. A
// key is different — the private half never leaves this machine — but the new
// address is still trusted on first sight, and from then on it is the machine
// homebutler reports about. That is a thing to be told, and a thing to be able
// to find afterwards, rather than a thing to refuse.
func (s *Server) noteMovedServer(cfg *config.Config, in serverInput) (move, notice string) {
	current := cfg.FindServer(in.Name)
	if current == nil || in.Remove {
		return "", ""
	}
	if in.Host == nil || *in.Host == current.Host {
		return "", ""
	}

	move = fmt.Sprintf("server %q address changed from %s to %s", in.Name, current.Host, *in.Host)
	if current.Password != "" {
		// The writer refuses this unless the password came with it, and a
		// password that came with it is not being carried anywhere.
		return move, ""
	}
	return move, in.Name + " now points at " + *in.Host + ", and that address is trusted the first time homebutler connects to it. Check it is the machine you mean."
}

func (s *Server) handleSaveProxmox(w http.ResponseWriter, r *http.Request) {
	var req proxmoxRequest
	if !decodeSave(w, r, &req) {
		return
	}
	if len(req.Endpoints) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	patch := config.Patch{Proxmox: make([]config.ProxmoxPatch, 0, len(req.Endpoints))}
	for _, in := range req.Endpoints {
		patch.Proxmox = append(patch.Proxmox, config.ProxmoxPatch{
			Name:          in.Name,
			Rename:        in.Rename,
			Host:          in.Host,
			Port:          in.Port,
			TokenID:       in.TokenID,
			Token:         in.Token.secret(),
			ActionTokenID: in.ActionTokenID,
			ActionToken:   in.ActionToken.secret(),
			Remove:        in.Remove,
		})
	}

	s.applySave(w, req.Revision, patch, []string{"watch"})
}
