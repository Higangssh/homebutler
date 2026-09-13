package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
)

// overviewDeadline bounds how long the whole overview waits. A server that is
// rebooting takes the full SSH dial timeout to fail, and one of those must not
// decide how long every other machine's reading takes to arrive. Anything still
// outstanding is reported from its last snapshot; the collection it was waiting
// on finishes in the background and lands in the cache for the next refresh.
const overviewDeadline = 4 * time.Second

// serverSnapshot is the last reading that could be taken from one server,
// stamped by the process that collected it rather than by the browser that
// receives it — the same rule #146 settled for Proxmox, and for the same
// reason: a timestamp the client invents cannot say data is stale.
type serverSnapshot struct {
	status    *system.StatusInfo
	updatedAt time.Time
}

// serverReading is one machine in the overview. Status is current, stale or
// unavailable; a stale entry carries the last reading that worked alongside
// the class of the failure that stopped a new one.
type serverReading struct {
	Name         string              `json:"name"`
	Host         string              `json:"host"`
	Local        bool                `json:"local"`
	Status       string              `json:"status"`
	UpdatedAt    *time.Time          `json:"updated_at,omitempty"`
	System       *system.StatusInfo  `json:"system,omitempty"`
	FailureClass remote.FailureClass `json:"failure_class,omitempty"`
	Message      string              `json:"message,omitempty"`
}

type overviewResponse struct {
	CollectedAt time.Time       `json:"collected_at"`
	Servers     []serverReading `json:"servers"`
}

// handleOverview reads every configured server at once.
//
// The card this replaces asked for the server list and then fetched each
// server's status one after another, so a dashboard with ten machines paid ten
// SSH round trips in series every fifteen seconds, and a host that was slow to
// answer stalled every machine behind it in the loop.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	// Each collection reports through a buffered channel rather than writing
	// into a shared slice. A goroutine that finishes after the deadline still
	// has somewhere to put its result and never touches what the response is
	// built from, so the late arrival races nothing — it only reaches the
	// cache, which is where the next refresh will find it.
	type result struct {
		index   int
		reading serverReading
	}
	results := make(chan result, len(s.cfg.Servers))

	for i := range s.cfg.Servers {
		go func(i int, srv config.ServerConfig) {
			results <- result{index: i, reading: s.readServer(&srv)}
		}(i, s.cfg.Servers[i])
	}

	readings := make([]serverReading, len(s.cfg.Servers))
	answered := make([]bool, len(s.cfg.Servers))
	deadline := time.After(overviewDeadline)

collect:
	for range s.cfg.Servers {
		select {
		case got := <-results:
			readings[got.index] = got.reading
			answered[got.index] = true
		case <-deadline:
			break collect
		case <-r.Context().Done():
			return
		}
	}

	for i, srv := range s.cfg.Servers {
		if !answered[i] {
			readings[i] = s.snapshotReading(&srv, "", "Still collecting. The last reading is shown.")
		}
	}

	writeJSON(w, overviewResponse{CollectedAt: time.Now().UTC(), Servers: readings})
}

// readServer takes one reading, caching it when it works and falling back to
// the last one that did when it does not.
func (s *Server) readServer(srv *config.ServerConfig) serverReading {
	status, err := s.collect(srv)
	if err != nil {
		return s.snapshotReading(srv, remote.Classify(err), "")
	}

	updatedAt := time.Now().UTC()
	s.serverMu.Lock()
	s.serverCache[srv.Name] = serverSnapshot{status: status, updatedAt: updatedAt}
	s.serverMu.Unlock()

	return serverReading{
		Name: srv.Name, Host: srv.Host, Local: srv.Local,
		Status: "current", UpdatedAt: &updatedAt, System: status,
	}
}

func (s *Server) collect(srv *config.ServerConfig) (*system.StatusInfo, error) {
	if srv.Local {
		return system.Status()
	}
	out, err := s.remoteRunner(srv, "status", "--json")
	if err != nil {
		return nil, err
	}
	var status system.StatusInfo
	if err := json.Unmarshal(out, &status); err != nil {
		// Connected, ran, and answered with something that is not a status.
		return nil, &remote.Error{Class: remote.ClassRemote, Err: err}
	}
	return &status, nil
}

// snapshotReading answers from the cache when there is something in it. The
// message is derived from the class rather than from the error: remote errors
// name the address, the config file, ~/.ssh/known_hosts and whatever the far
// side printed, none of which belongs in a browser.
func (s *Server) snapshotReading(srv *config.ServerConfig, class remote.FailureClass, message string) serverReading {
	if message == "" && class != "" {
		message = remote.Describe(class)
	}

	s.serverMu.RLock()
	snapshot, ok := s.serverCache[srv.Name]
	s.serverMu.RUnlock()

	if ok {
		return serverReading{
			Name: srv.Name, Host: srv.Host, Local: srv.Local,
			Status: "stale", UpdatedAt: &snapshot.updatedAt, System: snapshot.status,
			FailureClass: class, Message: message,
		}
	}
	return serverReading{
		Name: srv.Name, Host: srv.Host, Local: srv.Local,
		Status: "unavailable", FailureClass: class, Message: message,
	}
}
