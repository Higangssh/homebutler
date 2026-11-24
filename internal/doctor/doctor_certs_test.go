package doctor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
)

// tlsEndpoint serves a certificate valid until notAfter for names, and
// returns host:port. Generated here rather than taken from httptest so the
// expiry and the names are the test's to choose.
func tlsEndpoint(t *testing.T, notAfter time.Time, names ...string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_ = c.(*tls.Conn).Handshake()
				_ = c.Close()
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// runWithCerts runs doctor with the real probe against endpoints, on a
// clock the test chooses, and returns the exposure findings.
func runWithCerts(t *testing.T, cfg *config.Config, now time.Time) []Finding {
	t.Helper()
	fns := doctorFuncs(healthyStatus(), nil, nil, nil, nil)
	fns.CertProbeFn = probeCertificate
	r, err := Run(cfg, fns, Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var out []Finding
	for _, f := range r.Findings {
		if f.Category == "exposure" {
			out = append(out, f)
		}
	}
	return out
}

func onlyFinding(t *testing.T, findings []Finding) Finding {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("want one certificate finding, got %d: %+v", len(findings), findings)
	}
	return findings[0]
}

func TestCertificateExpiredFails(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(-3*24*time.Hour-time.Hour), "127.0.0.1")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))

	if f.Severity != SeverityFail || !strings.Contains(f.Title, "expired 3 days ago") {
		t.Fatalf("an expired certificate did not fail: %+v", f)
	}
	if !strings.Contains(f.Detail, "issued by 127.0.0.1") || !strings.HasPrefix(f.Command, "openssl s_client -connect "+addr) {
		t.Errorf("finding does not say who issued it or how to look: %+v", f)
	}
	if f.Runner != RunnerShell {
		t.Errorf("runner = %q, want shell", f.Runner)
	}
}

func TestCertificateInsideFourteenDaysWarns(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(5*24*time.Hour+time.Hour), "127.0.0.1")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))

	if f.Severity != SeverityWarn || !strings.Contains(f.Title, "expires in 5 days") {
		t.Fatalf("a certificate five days from expiry did not warn: %+v", f)
	}
}

func TestCertificateWithTimeLeftPasses(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(60*24*time.Hour+time.Hour), "127.0.0.1")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))

	if f.Severity != SeverityPass || !strings.Contains(f.Detail, addr+" (60 days left)") {
		t.Fatalf("a healthy certificate did not pass with its time left: %+v", f)
	}
}

// The boundary itself: fourteen days and a minute is not inside fourteen days.
func TestCertificateJustOutsideTheWindowPasses(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(certExpiryWarning+time.Minute), "127.0.0.1")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))
	if f.Severity != SeverityPass {
		t.Fatalf("got %+v", f)
	}
}

func TestCertificateForAnotherNameFails(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(60*24*time.Hour), "other.example.test")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))

	if f.Severity != SeverityFail || !strings.Contains(f.Title, "does not name 127.0.0.1") {
		t.Fatalf("a certificate for another name did not fail: %+v", f)
	}
	if !strings.Contains(f.Detail, "other.example.test") {
		t.Errorf("detail does not say what it does cover: %q", f.Detail)
	}
}

func TestCertificateUnreachableWarns(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, time.Now()))
	if f.Severity != SeverityWarn || !strings.HasPrefix(f.Title, "Could not read the certificate") {
		t.Fatalf("a closed port was not reported as unread: %+v", f)
	}
}

// A port that accepts and never speaks TLS must cost the dial timeout, not
// hang doctor.
func TestCertificateProbeGivesUpOnASilentPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan CertProbe, 1)
	go func() { done <- probeCertificate(ctx, CertTarget{Label: "silent", Host: host, Port: port}) }()
	select {
	case p := <-done:
		if p.Err == nil {
			t.Fatal("a port that never answered TLS produced a certificate")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the probe ignored its deadline and is still waiting on a silent port")
	}
}

// No list and no Proxmox is no finding — not a pass, and no dial.
func TestNoCertificatesConfiguredIsSilent(t *testing.T) {
	var dialled atomic.Int32
	fns := doctorFuncs(healthyStatus(), nil, nil, nil, nil)
	fns.CertProbeFn = func(context.Context, CertTarget) CertProbe {
		dialled.Add(1)
		return CertProbe{}
	}
	r, err := Run(&config.Config{}, fns, Options{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if strings.Contains(f.Title, "ertificate") {
			t.Errorf("a config with no endpoints produced %+v", f)
		}
	}
	if dialled.Load() != 0 {
		t.Errorf("dialled %d endpoint(s) with none configured", dialled.Load())
	}
}

func TestProxmoxEndpointsAreCheckedWithoutBeingListed(t *testing.T) {
	cfg := &config.Config{
		Certificates: []string{"home.example.test"},
		Proxmox: []config.ProxmoxConfig{
			{Name: "pinned", Host: "10.0.0.2", Fingerprint: strings.Repeat("ab", 32)},
			{Name: "trusted", Host: "pve.example.test", Port: 8443, CAFile: "/ca.pem"},
			{Name: "loose", Host: "10.0.0.3", Insecure: true},
		},
	}
	got := certTargets(cfg)
	want := []CertTarget{
		{Label: "home.example.test:443", Host: "home.example.test", Port: "443", CheckName: true},
		{Label: `Proxmox endpoint "pinned"`, Host: "10.0.0.2", Port: "8006", CheckName: false},
		{Label: `Proxmox endpoint "trusted"`, Host: "pve.example.test", Port: "8443", CheckName: true},
		{Label: `Proxmox endpoint "loose"`, Host: "10.0.0.3", Port: "8006", CheckName: false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A pinned Proxmox certificate is trusted by fingerprint, not by name, so a
// name it does not carry is not a finding — but its expiry still is.
func TestPinnedProxmoxCertificateIsNotFailedOnItsName(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(60*24*time.Hour), "pve.internal")
	host, port, _ := net.SplitHostPort(addr)
	p := probeCertificate(context.Background(), CertTarget{Host: host, Port: port, CheckName: false})
	if p.Err != nil || p.NameErr != nil {
		t.Fatalf("pinned endpoint probe = %+v", p)
	}

	expired := tlsEndpoint(t, now.Add(-time.Hour), "pve.internal")
	host, port, _ = net.SplitHostPort(expired)
	r := &Result{}
	checkCertificates(r, []CertProbe{probeCertificate(context.Background(), CertTarget{Label: "pve", Host: host, Port: port})}, now)
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityFail {
		t.Fatalf("an expired pinned certificate was not failed: %+v", r.Findings)
	}
}

func TestInvalidCertificateEntryIsReportedNotDialled(t *testing.T) {
	fns := doctorFuncs(healthyStatus(), nil, nil, nil, nil)
	fns.CertProbeFn = func(context.Context, CertTarget) CertProbe {
		t.Error("dialled an entry that does not parse")
		return CertProbe{}
	}
	r, err := Run(&config.Config{Certificates: []string{"https://home.example.test"}}, fns, Options{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(r, `Certificate endpoint "https://home.example.test" cannot be checked`) {
		t.Fatalf("no finding for an unparseable entry: %+v", r.Findings)
	}
}

// Results come back in config order however long each dial takes, and the
// dials overlap.
func TestCertificateProbesRunInParallelAndKeepTheirOrder(t *testing.T) {
	var targets []CertTarget
	for i := 0; i < 16; i++ {
		targets = append(targets, CertTarget{Label: string(rune('a' + i))})
	}
	start := time.Now()
	got := probeCertificates(targets, func(_ context.Context, target CertTarget) CertProbe {
		time.Sleep(time.Duration(16-int(target.Label[0]-'a')) * 10 * time.Millisecond)
		return CertProbe{Target: target}
	})
	for i, p := range got {
		if p.Target.Label != targets[i].Label {
			t.Fatalf("result %d is %q, want %q", i, p.Target.Label, targets[i].Label)
		}
	}
	// Sixteen sequential dials would take 1.36s.
	if elapsed := time.Since(start); elapsed > 800*time.Millisecond {
		t.Errorf("16 probes took %s; they are not overlapping", elapsed)
	}
}
