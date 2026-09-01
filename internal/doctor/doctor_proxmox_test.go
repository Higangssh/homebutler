package doctor

import (
	"fmt"
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

// startProxmoxServer starts a TLS test server and returns the host/port a
// proxmox.Client can dial with Insecure: true, matching how the real client
// talks to a self-signed Proxmox endpoint.
func startProxmoxServer(t *testing.T, handler http.HandlerFunc) (string, int) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

func openFnFor(host string, port int) func(config.ProxmoxConfig) (*proxmox.Client, error) {
	return func(config.ProxmoxConfig) (*proxmox.Client, error) {
		return proxmox.New(proxmox.Options{Host: host, Port: port, TokenID: "monitoring@pve!readonly", Token: "fixture-token", Insecure: true})
	}
}

func envelope(data string) string {
	return fmt.Sprintf(`{"data": %s}`, data)
}

func TestCheckProxmox_FullyReadable(t *testing.T) {
	host, port := startProxmoxServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/version":
			fmt.Fprint(w, envelope(`{"version":"9.0","release":"9.0"}`))
		case "/api2/json/cluster/status":
			fmt.Fprint(w, envelope(`[]`))
		case "/api2/json/cluster/resources":
			fmt.Fprint(w, envelope(`[{"type":"node","name":"pve1","status":"online"}]`))
		default:
			http.NotFound(w, r)
		}
	})

	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home"}}}
	r := &Result{}
	checkProxmox(r, cfg, openFnFor(host, port))

	f := findTitle(r.Findings, `Proxmox endpoint "home" is fully readable`)
	if f == nil || f.Severity != SeverityPass {
		t.Fatalf("expected pass finding, got: %#v", r.Findings)
	}
}

func TestCheckProxmox_EmptyResourcesIsNotAFailure(t *testing.T) {
	host, port := startProxmoxServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/version":
			fmt.Fprint(w, envelope(`{"version":"9.0","release":"9.0"}`))
		case "/api2/json/cluster/status":
			fmt.Fprint(w, envelope(`[]`))
		case "/api2/json/cluster/resources":
			fmt.Fprint(w, envelope(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home"}}}
	r := &Result{}
	checkProxmox(r, cfg, openFnFor(host, port))

	f := findTitle(r.Findings, `Proxmox endpoint "home" is reachable`)
	if f == nil || f.Severity != SeverityPass {
		t.Fatalf("expected pass finding for valid empty result, got: %#v", r.Findings)
	}
	if !strings.Contains(f.Detail, "resources") {
		t.Fatalf("expected detail to name the empty collector, got: %q", f.Detail)
	}
}

func TestCheckProxmox_PartialCollectorFailureIsWarn(t *testing.T) {
	host, port := startProxmoxServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/version":
			fmt.Fprint(w, envelope(`{"version":"9.0","release":"9.0"}`))
		case "/api2/json/cluster/status":
			http.Error(w, "Permission check failed (/, Sys.Audit)", http.StatusForbidden)
		case "/api2/json/cluster/resources":
			fmt.Fprint(w, envelope(`[{"type":"node","name":"pve1","status":"online"}]`))
		default:
			http.NotFound(w, r)
		}
	})

	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home"}}}
	r := &Result{}
	checkProxmox(r, cfg, openFnFor(host, port))

	f := findTitle(r.Findings, `Proxmox endpoint "home" is only partially readable`)
	if f == nil || f.Severity != SeverityWarn {
		t.Fatalf("expected warn finding, got: %#v", r.Findings)
	}
	if !strings.Contains(f.Action, "PVEAuditor") || !strings.Contains(f.Action, "Do not grant Administrator") {
		t.Fatalf("expected authorization action naming PVEAuditor and warning against Administrator, got: %q", f.Action)
	}
}

func TestCheckProxmox_NoReadableCollectorsIsFail(t *testing.T) {
	host, port := startProxmoxServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "authentication failure", http.StatusUnauthorized)
	})

	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home"}}}
	r := &Result{}
	checkProxmox(r, cfg, openFnFor(host, port))

	f := findTitle(r.Findings, `Proxmox endpoint "home" has no readable collectors`)
	if f == nil || f.Severity != SeverityFail {
		t.Fatalf("expected fail finding, got: %#v", r.Findings)
	}
	if !strings.Contains(f.Action, "token") {
		t.Fatalf("expected authentication action naming the token, got: %q", f.Action)
	}
}

func TestCheckProxmox_TokenFileUnreadableIsFail(t *testing.T) {
	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home", TokenFile: "/does/not/exist"}}}
	r := &Result{}
	checkProxmox(r, cfg, openProxmoxEndpoint)

	f := findTitle(r.Findings, `Proxmox endpoint "home" could not be configured`)
	if f == nil || f.Severity != SeverityFail {
		t.Fatalf("expected fail finding for unreadable token file, got: %#v", r.Findings)
	}
	if strings.Contains(f.Detail, "fixture-token") {
		t.Fatalf("finding must not contain a token value: %q", f.Detail)
	}
}

func TestCheckProxmox_NoEndpointsIsSilent(t *testing.T) {
	r := &Result{}
	checkProxmox(r, &config.Config{}, openProxmoxEndpoint)
	if len(r.Findings) != 0 {
		t.Fatalf("expected no findings without configured endpoints, got: %#v", r.Findings)
	}
	checkProxmox(r, nil, openProxmoxEndpoint)
	if len(r.Findings) != 0 {
		t.Fatalf("expected no findings for nil config, got: %#v", r.Findings)
	}
}
