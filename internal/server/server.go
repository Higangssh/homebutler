package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"path/filepath"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
)

//go:embed all:web_dist
var webFS embed.FS

// Server is the HTTP server for the homebutler web dashboard.
type Server struct {
	cfg     *config.Config
	host    string
	port    int
	demo    bool
	version string
	mux     *http.ServeMux
}

// New creates a new Server with the given config, host, port, and version.
func New(cfg *config.Config, host string, port int, demo ...bool) *Server {
	d := len(demo) > 0 && demo[0]
	if host == "" {
		host = "127.0.0.1"
	}
	s := &Server{cfg: cfg, host: host, port: port, demo: d, version: "dev", mux: http.NewServeMux()}
	s.routes()
	return s
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
	if s.demo {
		s.mux.HandleFunc("GET /api/status", s.cors(s.demoStatus))
		s.mux.HandleFunc("GET /api/docker", s.cors(s.demoDocker))
		s.mux.HandleFunc("GET /api/processes", s.cors(s.demoProcesses))
		s.mux.HandleFunc("GET /api/alerts", s.cors(s.demoAlerts))
		s.mux.HandleFunc("GET /api/ports", s.cors(s.demoPorts))
		s.mux.HandleFunc("GET /api/wake", s.cors(s.demoWake))
		s.mux.HandleFunc("POST /api/wake/{name}", s.cors(s.demoWakeSend))
		s.mux.HandleFunc("GET /api/servers", s.cors(s.demoServers))
		s.mux.HandleFunc("GET /api/servers/{name}/status", s.cors(s.demoServerStatus))
		s.mux.HandleFunc("GET /api/config", s.cors(s.demoConfig))
	} else {
		s.mux.HandleFunc("GET /api/status", s.cors(s.handleStatus))
		s.mux.HandleFunc("GET /api/docker", s.cors(s.handleDocker))
		s.mux.HandleFunc("GET /api/processes", s.cors(s.handleProcesses))
		s.mux.HandleFunc("GET /api/alerts", s.cors(s.handleAlerts))
		s.mux.HandleFunc("GET /api/ports", s.cors(s.handlePorts))
		s.mux.HandleFunc("GET /api/wake", s.cors(s.handleWakeList))
		s.mux.HandleFunc("POST /api/wake/{name}", s.cors(s.handleWakeSend))
		s.mux.HandleFunc("GET /api/servers", s.cors(s.handleServers))
		s.mux.HandleFunc("GET /api/servers/{name}/status", s.cors(s.handleServerStatus))
		s.mux.HandleFunc("GET /api/config", s.cors(s.handleConfig))
	}
	s.mux.HandleFunc("GET /api/version", s.cors(s.handleVersion))
	s.mux.HandleFunc("OPTIONS /api/", s.handleOptions)

	// Serve frontend static files
	s.mux.Handle("/", s.frontendHandler())
}

func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := fmt.Sprintf("http://%s:%d", s.host, s.port)
			if origin == allowed || origin == fmt.Sprintf("http://localhost:%d", s.port) || origin == fmt.Sprintf("http://127.0.0.1:%d", s.port) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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
	srv := s.cfg.FindServer(name)
	if srv == nil || srv.Local {
		return nil, false
	}
	return srv, true
}

// forwardRemote runs a homebutler subcommand on a remote server via SSH and writes the JSON response.
func (s *Server) forwardRemote(w http.ResponseWriter, srv *config.ServerConfig, args ...string) {
	out, err := remote.Run(srv, args...)
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
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "status", "--json")
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
		s.forwardRemote(w, srv, "docker", "list", "--json")
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
	result, err := alerts.Check(&s.cfg.Alerts)
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
	writeJSON(w, openPorts)
}

func (s *Server) handleWakeList(w http.ResponseWriter, r *http.Request) {
	targets := make([]map[string]string, len(s.cfg.Wake))
	for i, t := range s.cfg.Wake {
		targets[i] = map[string]string{
			"name": t.Name,
			"mac":  t.MAC,
		}
	}
	writeJSON(w, targets)
}

func (s *Server) handleWakeSend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	target := s.cfg.FindWakeTarget(name)
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
	servers := make([]map[string]any, len(s.cfg.Servers))
	for i, srv := range s.cfg.Servers {
		auth := srv.AuthMode
		if auth == "" {
			auth = "key"
		}
		key := ""
		if srv.KeyFile != "" {
			key = filepath.Base(srv.KeyFile)
		}
		pw := ""
		if srv.Password != "" {
			pw = "••••••"
		}
		servers[i] = map[string]any{
			"name":     srv.Name,
			"host":     srv.Host,
			"local":    srv.Local,
			"user":     srv.User,
			"port":     srv.Port,
			"auth":     auth,
			"key":      key,
			"password": pw,
		}
	}

	wakeTargets := make([]map[string]string, len(s.cfg.Wake))
	for i, t := range s.cfg.Wake {
		wakeTargets[i] = map[string]string{
			"name":      t.Name,
			"mac":       t.MAC,
			"broadcast": t.Broadcast,
		}
	}

	cfgPath := s.cfg.Path
	if cfgPath == "" {
		cfgPath = "(defaults)"
	}

	writeJSON(w, map[string]any{
		"path":    cfgPath,
		"servers": servers,
		"alerts": map[string]any{
			"cpu":    s.cfg.Alerts.CPU,
			"memory": s.cfg.Alerts.Memory,
			"disk":   s.cfg.Alerts.Disk,
		},
		"wake": wakeTargets,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": s.version})
}

// serverInfo is a safe subset of config.ServerConfig for the API response.
type serverInfo struct {
	Name  string `json:"name"`
	Host  string `json:"host"`
	Local bool   `json:"local"`
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	servers := make([]serverInfo, len(s.cfg.Servers))
	for i, srv := range s.cfg.Servers {
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
	srv := s.cfg.FindServer(name)
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

	// Run remotely via SSH
	out, err := remote.Run(srv, "status", "--json")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
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

func (s *Server) frontendHandler() http.Handler {
	// Check if embedded web_dist has content
	entries, err := webFS.ReadDir("web_dist")
	if err != nil || len(entries) == 0 {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(fallbackHTML))
		})
	}

	sub, err := fs.Sub(webFS, "web_dist")
	if err != nil {
		log.Printf("warning: failed to access embedded web files: %v", err)
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
<p>Web dashboard not built yet.</p>
<pre style="background:#161b22;padding:1em;border-radius:6px">make build-web</pre>
</div>
</body>
</html>`
