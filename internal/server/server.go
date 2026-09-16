package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"path/filepath"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/proxmox"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/service"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
	"github.com/Higangssh/homebutler/internal/watch"
)

//go:embed all:web_dist
var webFS embed.FS

// RemoteRunner executes a homebutler command on a remote server via SSH.
// Default implementation uses remote.Run.
type RemoteRunner func(srv *config.ServerConfig, args ...string) ([]byte, error)

// Server is the HTTP server for the homebutler web dashboard.
type Server struct {
	// cfg is replaced wholesale when a save reloads the file, while requests
	// are in flight reading it. The pointer swaps atomically and every handler
	// takes its own copy, so a reader either sees the config before the save
	// or the one after it, never a half-updated struct.
	cfg          atomic.Pointer[config.Config]
	host         string
	port         int
	demo         bool
	token        string
	version      string
	mux          *http.ServeMux
	revisionKey  []byte
	remoteRunner RemoteRunner
	proxmoxMu    sync.RWMutex
	proxmoxCache map[string]proxmoxSnapshot
	serverMu     sync.RWMutex
	serverCache  map[string]serverSnapshot
}

type proxmoxSnapshot struct {
	view      proxmox.DefaultView
	updatedAt time.Time
}

type proxmoxStatusResponse struct {
	proxmox.DefaultView
	Status                  string                          `json:"status"`
	UpdatedAt               *time.Time                      `json:"updated_at,omitempty"`
	FailureClass            proxmox.FailureClass            `json:"failure_class,omitempty"`
	Message                 string                          `json:"message,omitempty"`
	RefreshFailedCollectors []string                        `json:"refresh_failed_collectors,omitempty"`
	RefreshFailureClasses   map[string]proxmox.FailureClass `json:"refresh_failure_classes,omitempty"`
}

// New creates a new Server with the given config, host, port, and version.
func New(cfg *config.Config, host string, port int, demo ...bool) *Server {
	d := len(demo) > 0 && demo[0]
	if host == "" {
		host = "127.0.0.1"
	}
	s := &Server{host: host, port: port, demo: d, version: "dev", mux: http.NewServeMux(), remoteRunner: remote.Run, proxmoxCache: make(map[string]proxmoxSnapshot), serverCache: make(map[string]serverSnapshot)}
	s.cfg.Store(cfg)
	s.revisionKey = newRevisionKey()
	s.routes()
	return s
}

// config is the snapshot a request works from. Take it once at the top of a
// handler and use the local: calling it twice can straddle a save, and a
// response built from two different configs can disagree with itself.
func (s *Server) config() *config.Config {
	return s.cfg.Load()
}

// SetToken configures bearer token authentication for /api/* endpoints.
//
// The routes are rebuilt, because whether a write endpoint exists at all
// depends on there being a token and New runs before the caller sets one. A
// token arriving after the mux was built would otherwise leave every protected
// route unregistered on a dashboard that was started with one.
func (s *Server) SetToken(t string) {
	s.token = t
	s.mux = http.NewServeMux()
	s.routes()
}

// SetVersion sets the version string shown in the dashboard.
func (s *Server) SetVersion(v string) {
	s.version = v
}

// Handler returns the underlying http.Handler (for testing).
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Run starts the HTTP server.
func (s *Server) Run() error {
	if ports.IsPublicBind(s.host) {
		bound := s.host
		if bound == "" {
			bound = "all interfaces"
		}
		fmt.Fprintf(os.Stderr, "⚠️  WARNING: binding to %s exposes the dashboard to all network interfaces.\n", bound)
		if s.token == "" {
			fmt.Fprintln(os.Stderr, "   Consider using --token to require authentication.")
		}
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	displayAddr := fmt.Sprintf("http://%s:%d", s.host, s.port)

	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	serverErrors := make(chan error, 1)

	go func() {
		if s.demo {
			fmt.Printf("homebutler dashboard (DEMO MODE): %s\n", displayAddr)
		} else {
			fmt.Printf("homebutler dashboard: %s\n", displayAddr)
		}
		serverErrors <- srv.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && strings.Contains(err.Error(), "address already in use") {
			return fmt.Errorf("port %d is already in use. Try a different port:\n  homebutler serve --port %d", s.port, s.port+1)
		}
		return err

	case sig := <-shutdown:
		log.Printf("\nReceived signal: %v. Starting graceful shutdown...\n", sig)

		// 5 second timeout to allow active connections to drain
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			srv.Close()
			return fmt.Errorf("could not stop server gracefully: %w", err)
		}
		log.Println("Server stopped")
	}

	return nil
}

func (s *Server) routes() {
	// api wraps handlers with CORS and optional bearer token auth.
	api := func(h http.HandlerFunc) http.HandlerFunc {
		return s.requireAuth(s.cors(h))
	}

	// The method and path of anything that is a capability come from the
	// registry rather than from this list, so an endpoint the dashboard can
	// reach is one the registry says it can reach. This file supplies the
	// implementation and nothing else; TestEveryExposedCapabilityHasAHandler
	// pins that the two sets match, in both modes.
	handlers := s.capabilityHandlers()
	for _, c := range capability.Registry {
		if !c.Exposed() {
			continue
		}
		// A capability that says it needs a token is not registered without
		// one. Answering "unauthorized" would leave the surface there to be
		// reached the moment that check was got wrong; a route that does not
		// exist cannot be.
		if c.HTTP.Protection != capability.ProtectionNone && s.token == "" {
			continue
		}
		if h, ok := handlers[c.Tool.Name]; ok {
			s.mux.HandleFunc(c.HTTP.Method+" "+c.HTTP.Path, api(h))
		}
	}

	// Endpoints that are not capabilities: they describe this server rather
	// than something homebutler can do to a machine, so the registry has
	// nothing to say about them.
	if s.demo {
		s.mux.HandleFunc("GET /api/wake", api(s.demoWake))
		s.mux.HandleFunc("GET /api/overview", api(s.demoOverview))
		s.mux.HandleFunc("GET /api/servers", api(s.demoServers))
		s.mux.HandleFunc("GET /api/servers/{name}/status", api(s.demoServerStatus))
		s.mux.HandleFunc("GET /api/config", api(s.demoConfig))
		s.mux.HandleFunc("GET /api/watch/incidents/{id}", api(s.demoWatchIncident))
		s.mux.HandleFunc("GET /api/report", api(s.demoReport))
	} else {
		s.mux.HandleFunc("GET /api/wake", api(s.handleWakeList))
		s.mux.HandleFunc("GET /api/overview", api(s.handleOverview))
		s.mux.HandleFunc("GET /api/servers", api(s.handleServers))
		s.mux.HandleFunc("GET /api/servers/{name}/status", api(s.handleServerStatus))
		s.mux.HandleFunc("GET /api/config", api(s.handleConfig))
		s.mux.HandleFunc("GET /api/watch/incidents/{id}", api(s.handleWatchIncident))
		s.mux.HandleFunc("GET /api/report", api(s.handleReport))
	}
	// Write endpoints exist only when a token does. Registering them behind a
	// check that says "unauthorized" would still be a write surface on an
	// unauthenticated dashboard the moment that check was got wrong; a route
	// that was never registered cannot be reached by getting anything wrong.
	// #154 decided this: a read-only dashboard on 127.0.0.1 without a token is
	// defensible, and the same page able to rewrite the config is not.
	if s.token != "" {
		if s.demo {
			s.mux.HandleFunc("PUT /api/config/alerts", api(s.demoSaveAlerts))
			s.mux.HandleFunc("PUT /api/config/notify", api(s.demoSaveNotify))
			s.mux.HandleFunc("PUT /api/config/wake", api(s.demoSaveWake))
			s.mux.HandleFunc("PUT /api/config/servers", api(s.demoSaveServers))
			s.mux.HandleFunc("PUT /api/config/proxmox", api(s.demoSaveProxmox))
		} else {
			s.mux.HandleFunc("PUT /api/config/alerts", api(s.handleSaveAlerts))
			s.mux.HandleFunc("PUT /api/config/notify", api(s.handleSaveNotify))
			s.mux.HandleFunc("PUT /api/config/wake", api(s.handleSaveWake))
			s.mux.HandleFunc("PUT /api/config/servers", api(s.handleSaveServers))
			s.mux.HandleFunc("PUT /api/config/proxmox", api(s.handleSaveProxmox))
		}
	}

	s.mux.HandleFunc("GET /api/proxmox/endpoints", api(s.handleProxmoxEndpoints))
	s.mux.HandleFunc("GET /api/capabilities", api(s.handleCapabilities))
	s.mux.HandleFunc("GET /api/version", api(s.handleVersion))
	s.mux.HandleFunc("OPTIONS /api/", s.handleOptions)

	// An /api/ path that matched nothing is a missing endpoint, and saying so
	// is the whole point of not registering the write routes without a token:
	// falling through to the single-page app answered 200 with HTML, which a
	// caller cannot tell from a working endpoint until it tries to parse it.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})

	// Serve frontend static files
	s.mux.Handle("/", frontendHandler(webFS))
}

// capabilityHandlers maps a capability to what answers for it here. Demo mode
// answers the same paths from fixed data, which is what makes the end-to-end
// suite deterministic, so the split is over handlers rather than over routes.
func (s *Server) capabilityHandlers() map[string]http.HandlerFunc {
	if s.demo {
		return map[string]http.HandlerFunc{
			"system_status":  s.demoStatus,
			"docker_list":    s.demoDocker,
			"docker_stats":   s.demoDockerStats,
			"processes":      s.demoProcesses,
			"alerts":         s.demoAlerts,
			"open_ports":     s.demoPorts,
			"wake":           s.demoWakeSend,
			"proxmox_status": s.handleProxmoxStatus,
			"watch_list":     s.demoWatch,
			"watch_history":  s.demoWatchIncidents,
			"notify_test":    s.demoNotifyTest,
			"report":         s.demoReportSnapshot,
			"doctor":         s.demoDoctor,
		}
	}
	return map[string]http.HandlerFunc{
		"system_status":  s.handleStatus,
		"docker_list":    s.handleDocker,
		"docker_stats":   s.handleDockerStats,
		"processes":      s.handleProcesses,
		"alerts":         s.handleAlerts,
		"open_ports":     s.handlePorts,
		"wake":           s.handleWakeSend,
		"proxmox_status": s.handleProxmoxStatus,
		"watch_list":     s.handleWatch,
		"watch_history":  s.handleWatchIncidents,
		"notify_test":    s.handleNotifyTest,
		"report":         s.handleReportSnapshot,
		"doctor":         s.handleDoctor,
	}
}

// capabilityInfo is one row of what homebutler can do, as the dashboard needs
// to know it: what it is called, what it costs to call, what it can be pointed
// at, and whether this interface can reach it at all.
type capabilityInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Risk        string   `json:"risk"`
	Targets     []string `json:"targets"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	Absent      string   `json:"absent,omitempty"`
}

// handleCapabilities answers what homebutler can do and how much of it this
// interface reaches. The absent half is the point: MCP has forty tools and the
// dashboard reaches ten, and until now nothing said so.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	out := make([]capabilityInfo, 0, len(capability.Registry))
	for _, c := range capability.Registry {
		targets := make([]string, 0, len(c.Targets))
		for _, t := range c.Targets {
			targets = append(targets, string(t))
		}
		out = append(out, capabilityInfo{
			Name:        c.Tool.Name,
			Description: c.Tool.Description,
			Risk:        string(c.Risk),
			Targets:     targets,
			Method:      c.HTTP.Method,
			Path:        c.HTTP.Path,
			Absent:      c.HTTP.Absent,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := fmt.Sprintf("http://%s:%d", s.host, s.port)
			if origin == allowed || origin == fmt.Sprintf("http://localhost:%d", s.port) || origin == fmt.Sprintf("http://127.0.0.1:%d", s.port) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
		}
		next(w, r)
	}
}

// requireAuth wraps a handler and enforces bearer token authentication
// on /api/* paths when a token is configured.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && strings.HasPrefix(r.URL.Path, "/api/") {
			auth := r.Header.Get("Authorization")
			expected := "Bearer " + s.token
			if len(auth) != len(expected) || subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

// isRemoteRequest checks the ?server query param and returns the server config if it's a remote server.
// Returns (nil, false) if no server param, server is local, or server not found.
func (s *Server) isRemoteRequest(r *http.Request) (*config.ServerConfig, bool) {
	name := r.URL.Query().Get("server")
	if name == "" {
		return nil, false
	}
	srv := s.config().FindServer(name)
	if srv == nil || srv.Local {
		return nil, false
	}
	return srv, true
}

// forwardRemote runs a homebutler subcommand on a remote server via SSH and writes the JSON response.
func (s *Server) forwardRemote(w http.ResponseWriter, srv *config.ServerConfig, args ...string) {
	out, err := s.remoteRunner(srv, args...)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var raw json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		writeError(w, http.StatusBadGateway, "invalid response from remote server")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		allowed := fmt.Sprintf("http://%s:%d", s.host, s.port)
		if origin == allowed || origin == fmt.Sprintf("http://localhost:%d", s.port) || origin == fmt.Sprintf("http://127.0.0.1:%d", s.port) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "status", "--json")
		return
	}
	// Inside a container this would be the container's own /proc, which is a
	// convincing answer about the wrong machine.
	if system.InContainer() {
		writeError(w, http.StatusNotImplemented, system.ContainerCannotSeeHost)
		return
	}
	info, err := system.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleDocker(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		out, err := s.remoteRunner(srv, "docker", "list", "--json")
		if err != nil {
			writeJSON(w, map[string]any{
				"available":  false,
				"message":    err.Error(),
				"containers": []any{},
			})
			return
		}
		var containers json.RawMessage
		if err := json.Unmarshal(out, &containers); err != nil {
			writeJSON(w, map[string]any{
				"available":  false,
				"message":    "invalid response from remote server",
				"containers": []any{},
			})
			return
		}
		writeJSON(w, map[string]any{
			"available":  true,
			"containers": containers,
		})
		return
	}
	containers, err := docker.List()
	if err != nil {
		// Return empty list with unavailable status instead of raw error
		writeJSON(w, map[string]any{
			"available":  false,
			"message":    "Docker is not available",
			"containers": []any{},
		})
		return
	}
	writeJSON(w, map[string]any{
		"available":  true,
		"containers": containers,
	})
}

func (s *Server) handleDockerStats(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		out, err := s.remoteRunner(srv, "docker", "stats", "--json")
		if err != nil {
			writeJSON(w, map[string]any{
				"available": false,
				"message":   err.Error(),
				"stats":     []any{},
			})
			return
		}
		var stats json.RawMessage
		if err := json.Unmarshal(out, &stats); err != nil {
			writeJSON(w, map[string]any{
				"available": false,
				"message":   "invalid response from remote server",
				"stats":     []any{},
			})
			return
		}
		writeJSON(w, map[string]any{
			"available": true,
			"stats":     stats,
		})
		return
	}
	stats, err := docker.Stats()
	if err != nil {
		writeJSON(w, map[string]any{
			"available": false,
			"message":   "Docker is not available",
			"stats":     []any{},
		})
		return
	}
	writeJSON(w, map[string]any{
		"available": true,
		"stats":     stats,
	})
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "processes", "--json")
		return
	}
	procs, err := system.TopProcesses(10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, procs)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "alerts", "--json")
		return
	}
	result, err := alerts.Check(&s.config().Alerts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "ports", "--json")
		return
	}
	openPorts, err := ports.List()
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	// The same correlation report makes, so the dashboard does not call a port
	// anonymous that the Report tab names two panels away.
	if containers, dockerErr := docker.List(); dockerErr == nil {
		openPorts.Ports = inventory.AttributePorts(openPorts.Ports, containers)
	}
	writeJSON(w, openPorts)
}

func (s *Server) handleWakeList(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	targets := make([]map[string]string, len(cfg.Wake))
	for i, t := range cfg.Wake {
		targets[i] = map[string]string{
			"name": t.Name,
			"mac":  t.MAC,
		}
	}
	writeJSON(w, targets)
}

func (s *Server) handleWakeSend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	target := s.config().FindWakeTarget(name)
	if target == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("wake target %q not found", name))
		return
	}

	broadcast := target.Broadcast
	if broadcast == "" {
		broadcast = "255.255.255.255"
	}

	result, err := wake.Send(target.MAC, broadcast)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	servers := make([]map[string]any, len(cfg.Servers))
	for i, srv := range cfg.Servers {
		auth := srv.AuthMode
		if auth == "" {
			auth = "key"
		}
		key := ""
		if srv.KeyFile != "" {
			key = filepath.Base(srv.KeyFile)
		}
		// Whether a password is set, never a stand-in for one. "••••••" is a
		// value shaped like a credential, and a caller sending it back would
		// write the bullets into the file as the password.
		hasPassword := srv.Password != ""
		servers[i] = map[string]any{
			"name":         srv.Name,
			"host":         srv.Host,
			"local":        srv.Local,
			"user":         srv.User,
			"port":         srv.Port,
			"auth":         auth,
			"key":          key,
			"password_set": hasPassword,
		}
	}

	wakeTargets := make([]map[string]string, len(cfg.Wake))
	for i, t := range cfg.Wake {
		wakeTargets[i] = map[string]string{
			"name":      t.Name,
			"mac":       t.MAC,
			"broadcast": t.Broadcast,
		}
	}

	cfgPath := cfg.Path
	if cfgPath == "" {
		cfgPath = "(defaults)"
	}

	editable := s.token != ""

	// Proxmox endpoints, as a form needs them: the token ids are not secret —
	// they name a user, not a credential — and whether each token is set is a
	// flag, never the token. An endpoint that keeps its token in a file says
	// so, because that is the one the dashboard cannot change the address of.
	endpoints := make([]map[string]any, len(cfg.Proxmox))
	for i, endpoint := range cfg.Proxmox {
		endpoints[i] = map[string]any{
			"name":                 endpoint.Name,
			"host":                 endpoint.Host,
			"port":                 endpoint.Port,
			"token_id":             endpoint.TokenID,
			"token_set":            endpoint.Token != "" || endpoint.TokenFile != "",
			"token_in_file":        endpoint.TokenFile != "",
			"action_token_id":      endpoint.ActionTokenID,
			"action_token_set":     endpoint.ActionToken != "" || endpoint.ActionTokenFile != "",
			"action_token_in_file": endpoint.ActionTokenFile != "",
		}
	}

	body := map[string]any{
		"path":    cfgPath,
		"servers": servers,
		"proxmox": endpoints,
		"alerts": map[string]any{
			"cpu":    cfg.Alerts.CPU,
			"memory": cfg.Alerts.Memory,
			"disk":   cfg.Alerts.Disk,
		},
		"wake":   wakeTargets,
		"notify": s.notifySettings(cfg),
		// Whether this dashboard can write at all, so the UI offers editing
		// only where it exists rather than offering it and failing.
		"editable":          editable,
		"transport_warning": s.transportWarning(),
	}

	// The revision this page was built from comes back with a save, so an edit
	// made in an editor meanwhile is not silently overwritten. A dashboard
	// that cannot write has nothing to send it back with, so it does not get
	// one — a digest of the file is not something to hand out for no reason.
	if editable {
		if rev, err := config.ReadRevision(cfg.Path); err == nil {
			body["revision"] = s.revisionToken(rev)
		}
	}

	writeJSON(w, body)
}

// watchService is what doctor checks and the dashboard now shows: whether a
// unit exists to poll the watch list. Installed is about the file, not whether
// the supervisor has it running.
type watchService struct {
	Installed bool   `json:"installed"`
	Unit      string `json:"unit,omitempty"`
}

// watchRetention says how full the incident directory is. Max is 0 when
// history was made unlimited on purpose, which is a choice rather than a
// state to warn about.
type watchRetention struct {
	Kept int `json:"kept"`
	Max  int `json:"max"`
}

type watchOverview struct {
	Targets   []watch.Target `json:"targets"`
	Service   watchService   `json:"service"`
	Retention watchRetention `json:"retention"`
}

// handleWatch answers what is being watched, whether anything is installed to
// poll it, and how much history is being kept. The three are one request
// because a target list is misleading on its own: entries with no service is
// the state where every other monitoring feature is silent.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	dir, err := watch.WatchDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	targets, err := watch.LoadTargets(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if targets == nil {
		targets = []watch.Target{}
	}

	installed, unit := service.InstalledUnit()

	// A directory that cannot be listed is reported as no incidents kept
	// rather than failing the whole view: the target list is the part that
	// answers "is anything watched at all".
	kept, _ := watch.CountIncidents(dir)

	writeJSON(w, watchOverview{
		Targets: targets,
		Service: watchService{Installed: installed, Unit: unit},
		Retention: watchRetention{
			Kept: kept,
			Max:  s.config().Watch.Retention.MaxIncidents,
		},
	})
}

// handleWatchIncidents lists recorded incidents, newest first, without logs.
// Every incident carries two hundred lines of captured output, so the list
// stays cheap and a single incident is fetched whole when one is opened.
func (s *Server) handleWatchIncidents(w http.ResponseWriter, r *http.Request) {
	dir, err := watch.WatchDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	incidents, err := watch.History(dir, watch.HistoryOptions{
		Limit:     limit,
		Container: r.URL.Query().Get("container"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if incidents == nil {
		incidents = []watch.Incident{}
	}
	writeJSON(w, incidents)
}

// handleWatchIncident returns one incident with the logs captured around it.
// The pre-restart logs are the reason watch takes them before the container
// dies rather than reading them afterwards, so they are the point of opening
// an incident at all.
func (s *Server) handleWatchIncident(w http.ResponseWriter, r *http.Request) {
	dir, err := watch.WatchDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incident, err := watch.LoadIncident(dir, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, incident)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": s.version})
}

type proxmoxEndpointInfo struct {
	Name string `json:"name"`
}

func (s *Server) handleProxmoxEndpoints(w http.ResponseWriter, _ *http.Request) {
	cfg := s.config()
	endpoints := make([]proxmoxEndpointInfo, 0, len(cfg.Proxmox))
	if !s.demo {
		for _, endpoint := range cfg.Proxmox {
			endpoints = append(endpoints, proxmoxEndpointInfo{Name: endpoint.Name})
		}
	}
	writeJSON(w, endpoints)
}

func (s *Server) handleProxmoxStatus(w http.ResponseWriter, r *http.Request) {
	if s.demo {
		writeError(w, http.StatusNotFound, "no Proxmox endpoints configured in demo mode")
		return
	}

	endpoint, err := s.config().SelectProxmox(r.URL.Query().Get("endpoint"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tokenID, token, err := endpoint.ResolveCredential(false)
	if err != nil {
		log.Print(err)
		s.writeProxmoxFailure(w, endpoint.Name, proxmox.Classify(err), nil, nil)
		return
	}
	client, err := proxmox.New(proxmox.Options{
		Host: endpoint.Host, Port: endpoint.APIPort(), TokenID: tokenID, Token: token,
		Fingerprint: endpoint.Fingerprint, CAFile: endpoint.CAFile, Insecure: endpoint.Insecure, Timeout: endpoint.TimeoutDuration(),
	})
	if err != nil {
		log.Printf("configure Proxmox endpoint %q: %v", endpoint.Name, err)
		s.writeProxmoxFailure(w, endpoint.Name, proxmox.Classify(err), nil, nil)
		return
	}
	view, err := client.DefaultView(r.Context())
	if err != nil {
		s.writeProxmoxFailure(w, endpoint.Name, proxmox.Classify(err), nil, nil)
		return
	}
	class := proxmox.Classify(view.FirstErr)
	if view.CollectorFailed(proxmox.CollectorVersion) && view.CollectorFailed(proxmox.CollectorCluster) && view.CollectorFailed(proxmox.CollectorResources) {
		s.writeProxmoxFailure(w, endpoint.Name, class, view.Failed, view.FailureClasses)
		return
	}

	updatedAt := time.Now().UTC()
	status := "current"
	if len(view.Failed) > 0 {
		status = "partially_readable"
	}
	s.proxmoxMu.Lock()
	s.proxmoxCache[endpoint.Name] = proxmoxSnapshot{view: view, updatedAt: updatedAt}
	s.proxmoxMu.Unlock()
	writeJSON(w, proxmoxStatusResponse{DefaultView: view, Status: status, UpdatedAt: &updatedAt, FailureClass: class})
}

func (s *Server) writeProxmoxFailure(w http.ResponseWriter, endpoint string, class proxmox.FailureClass, failed []string, failureClasses map[string]proxmox.FailureClass) {
	message := ""
	if class == "" {
		message = "Proxmox status is unavailable"
	}
	s.proxmoxMu.RLock()
	snapshot, ok := s.proxmoxCache[endpoint]
	s.proxmoxMu.RUnlock()
	if ok {
		writeJSON(w, proxmoxStatusResponse{DefaultView: snapshot.view, Status: "stale", UpdatedAt: &snapshot.updatedAt, FailureClass: class, Message: message, RefreshFailedCollectors: failed, RefreshFailureClasses: failureClasses})
		return
	}
	writeJSON(w, proxmoxStatusResponse{DefaultView: proxmox.DefaultView{Failed: failed, FailureClasses: failureClasses}, Status: "unavailable", FailureClass: class, Message: message})
}

// serverInfo is a safe subset of config.ServerConfig for the API response.
type serverInfo struct {
	Name  string `json:"name"`
	Host  string `json:"host"`
	Local bool   `json:"local"`
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	servers := make([]serverInfo, len(cfg.Servers))
	for i, srv := range cfg.Servers {
		servers[i] = serverInfo{
			Name:  srv.Name,
			Host:  srv.Host,
			Local: srv.Local,
		}
	}
	writeJSON(w, servers)
}

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	srv := s.config().FindServer(name)
	if srv == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("server %q not found", name))
		return
	}

	if srv.Local {
		// Run locally
		info, err := system.Status()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, info)
		return
	}

	// Run remotely via SSH. The error is not passed through: remote failures
	// name the address, the config file, ~/.ssh/known_hosts and whatever the
	// far side printed, which is right for a terminal and wrong for a browser.
	// What crosses is the class and the sentence that goes with it.
	out, err := s.remoteRunner(srv, "status", "--json")
	if err != nil {
		log.Print(err)
		writeError(w, http.StatusBadGateway, remote.Describe(remote.Classify(err)))
		return
	}

	// Validate it's JSON before forwarding
	var raw json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		writeError(w, http.StatusBadGateway, "invalid response from remote server")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// frontendHandler serves the dashboard embedded under web_dist in web. A binary
// built without the dashboard still embeds the tracked .gitkeep, so "built"
// means index.html is present, not that the directory has entries.
func frontendHandler(web fs.FS) http.Handler {
	sub, err := fs.Sub(web, "web_dist")
	if err == nil {
		_, err = fs.Stat(sub, "index.html")
	}
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(fallbackHTML))
		})
	}

	fileServer := http.FileServer(http.FS(sub))

	// SPA fallback: serve index.html for paths that don't match a file
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try to open the file; if it doesn't exist, serve index.html
		if !strings.HasPrefix(path, "/api/") {
			f, err := sub.Open(strings.TrimPrefix(path, "/"))
			if err != nil {
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

const fallbackHTML = `<!DOCTYPE html>
<html>
<head><title>homebutler</title></head>
<body style="background:#0d1117;color:#c9d1d9;font-family:monospace;display:flex;justify-content:center;align-items:center;height:100vh;margin:0">
<div style="text-align:center">
<h1 style="color:#58a6ff">homebutler</h1>
<p>This binary was built without the web dashboard.</p>
<p>Installed with <code>go install</code>? The <a href="https://github.com/Higangssh/homebutler/releases" style="color:#58a6ff">release binaries</a> ship it:</p>
<pre style="background:#161b22;padding:1em;border-radius:6px">curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh | sh</pre>
<p>Building from a checkout? Compile the dashboard in and rebuild:</p>
<pre style="background:#161b22;padding:1em;border-radius:6px">make build-all</pre>
</div>
</body>
</html>`
