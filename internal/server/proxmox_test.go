package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/proxmox"
)

func TestProxmoxEndpointsHideSecrets(t *testing.T) {
	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{
		{Name: "pve-a", Host: "pve-a.example", TokenID: "monitoring@pve!readonly", Token: "secret-a", TokenFile: "/secret/token-a", CAFile: "/secret/ca.pem"},
		{Name: "pve-b", Host: "pve-b.example", TokenID: "monitoring@pve!readonly", Token: "secret-b"},
	}}
	recorder := httptest.NewRecorder()
	New(cfg, "127.0.0.1", 8080).Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/proxmox/endpoints", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got, want := strings.TrimSpace(recorder.Body.String()), `[{"name":"pve-a"},{"name":"pve-b"}]`; got != want {
		t.Fatalf("response = %s, want %s", got, want)
	}
}

func TestProxmoxStatusSelectsEndpointAndPreservesPartialResults(t *testing.T) {
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "PVEAPIToken=monitoring@pve!readonly=test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		switch r.URL.Path {
		case "/api2/json/version":
			_, _ = w.Write([]byte(`{"data":{"version":"8.2.0","release":"8.2"}}`))
		case "/api2/json/cluster/status":
			http.Error(w, "permission denied", http.StatusForbidden)
		case "/api2/json/cluster/resources":
			_, _ = w.Write([]byte(`{"data":[{"type":"node","node":"pve1","status":"online","cpu":0.25,"maxcpu":8,"mem":4294967296,"maxmem":17179869184,"uptime":86400},{"type":"qemu","vmid":100,"node":"pve1","status":"running","name":"web"},{"type":"lxc","vmid":101,"node":"pve1","status":"stopped","name":"dns"},{"type":"storage","storage":"local-lvm","node":"pve1","status":"available","disk":53687091200,"maxdisk":107374182400}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	apiURL, err := url.Parse(api.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(apiURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{
		{Name: "other", Host: "other.invalid", TokenID: "other@pve!readonly", Token: "other-token", Insecure: true},
		{Name: "pve", Host: host, Port: port, TokenID: "monitoring@pve!readonly", Token: "test-token", Insecure: true},
	}}
	srv := New(cfg, "127.0.0.1", 8080)

	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/proxmox/status", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status without endpoint = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	recorder = httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/proxmox/status?endpoint=pve", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var view proxmox.DefaultView
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Version.Version != "8.2.0" || len(view.Resources.Nodes) != 1 || len(view.Resources.Guests) != 2 || len(view.Resources.Storage) != 1 {
		t.Fatalf("view = %#v", view)
	}
	if !view.CollectorFailed(proxmox.CollectorCluster) || len(view.Warnings) != 1 {
		t.Fatalf("partial failure = %#v", view)
	}
	if strings.Contains(recorder.Body.String(), "test-token") {
		t.Fatal("response leaked the Proxmox token")
	}

	cfg.Proxmox = cfg.Proxmox[1:]
	recorder = httptest.NewRecorder()
	New(cfg, "127.0.0.1", 8080).Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/proxmox/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("single endpoint status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestProxmoxStatusHidesCredentialPaths(t *testing.T) {
	tests := []struct {
		name     string
		endpoint config.ProxmoxConfig
		secret   string
	}{
		{name: "token file", endpoint: config.ProxmoxConfig{Name: "pve", Host: "pve.example", TokenID: "monitoring@pve!readonly", TokenFile: "/sensitive/proxmox-token"}, secret: "/sensitive/proxmox-token"},
		{name: "CA file", endpoint: config.ProxmoxConfig{Name: "pve", Host: "pve.example", TokenID: "monitoring@pve!readonly", Token: "test-token", CAFile: "/sensitive/proxmox-ca.pem"}, secret: "/sensitive/proxmox-ca.pem"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			cfg := &config.Config{Proxmox: []config.ProxmoxConfig{test.endpoint}}
			New(cfg, "127.0.0.1", 8080).Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/proxmox/status", nil))

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
			if strings.Contains(recorder.Body.String(), test.secret) {
				t.Fatalf("response leaked credential path %q", test.secret)
			}
		})
	}
}
