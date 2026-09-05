package doctor

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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

// An empty resources response is a warning, not a pass: proxmox status and
// the resources collector it shares with doctor both treat "no nodes, no
// guests, no storage" as a collector that failed to prove anything, usually
// an ACL-limited token rather than a genuinely empty cluster. doctor must
// agree, or the two commands disagree about the same endpoint.
func TestCheckProxmox_EmptyResourcesIsWarn(t *testing.T) {
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

	f := findTitle(r.Findings, `Proxmox endpoint "home" is only partially readable`)
	if f == nil || f.Severity != SeverityWarn {
		t.Fatalf("expected warn finding for a token that sees no resources, got: %#v", r.Findings)
	}
	if !strings.Contains(f.Detail, "resources") || !strings.Contains(f.Detail, "token permissions") {
		t.Fatalf("expected detail to name the empty collector and point at token permissions, got: %q", f.Detail)
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
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced the same way on Windows")
	}

	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	const secret = "should-never-appear-in-a-finding"
	if err := os.WriteFile(tokenFile, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tokenFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tokenFile, 0o600) })

	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home", TokenFile: tokenFile}}}
	r := &Result{}
	checkProxmox(r, cfg, openProxmoxEndpoint)

	f := findTitle(r.Findings, `Proxmox endpoint "home" could not be configured`)
	if f == nil || f.Severity != SeverityFail {
		t.Fatalf("expected fail finding for unreadable token file, got: %#v", r.Findings)
	}
	if strings.Contains(f.Detail, secret) {
		t.Fatalf("finding must not contain a token value: %q", f.Detail)
	}
}

func TestCheckProxmox_ConfigurationFailureNamesFields(t *testing.T) {
	cfg := &config.Config{Proxmox: []config.ProxmoxConfig{{Name: "home"}}}
	r := &Result{}
	checkProxmox(r, cfg, func(config.ProxmoxConfig) (*proxmox.Client, error) {
		return nil, proxmox.WithFailureClass(proxmox.FailureConfiguration, fmt.Errorf("proxmox host is required"))
	})

	f := findTitle(r.Findings, `Proxmox endpoint "home" could not be configured`)
	if f == nil || f.Severity != SeverityFail {
		t.Fatalf("expected configuration finding, got: %#v", r.Findings)
	}
	if !strings.Contains(f.Action, "host, port, timeout") || !strings.Contains(f.Action, "CA file") {
		t.Fatalf("expected configuration action naming fields, got: %q", f.Action)
	}
}

func TestProxmoxFailureAction_Response(t *testing.T) {
	action := proxmoxFailureAction(proxmox.WithFailureClass(proxmox.FailureResponse, fmt.Errorf("decode Proxmox response")))
	if !strings.Contains(action, "Proxmox API") || !strings.Contains(action, "reverse-proxy") {
		t.Fatalf("response action = %q", action)
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
