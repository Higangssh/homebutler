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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
)

// certSpec describes the certificate a test endpoint presents.
type certSpec struct {
	names     []string // SANs; the first is also the CN unless cn is set
	cn        string
	notBefore time.Time // zero means 90 days before notAfter
	notAfter  time.Time
	// intermediateNotAfter, when set, puts the leaf under an intermediate
	// that ends then, presented in the chain.
	intermediateNotAfter time.Time
}

type certKey struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

func makeCert(t *testing.T, tmpl *x509.Certificate, parent *certKey) certKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	parentCert, parentKey := tmpl, key
	if parent != nil {
		parentCert, parentKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certKey{cert: c, der: der, key: key}
}

func (s certSpec) chain(t *testing.T) tls.Certificate {
	t.Helper()
	notBefore := s.notBefore
	if notBefore.IsZero() {
		notBefore = s.notAfter.Add(-90 * 24 * time.Hour)
	}
	cn := s.cn
	if cn == "" && len(s.names) > 0 {
		cn = s.names[0]
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     s.notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range s.names {
		if ip := net.ParseIP(n); ip != nil {
			leafTmpl.IPAddresses = append(leafTmpl.IPAddresses, ip)
		} else {
			leafTmpl.DNSNames = append(leafTmpl.DNSNames, n)
		}
	}
	if s.intermediateNotAfter.IsZero() {
		leaf := makeCert(t, leafTmpl, nil)
		return tls.Certificate{Certificate: [][]byte{leaf.der}, PrivateKey: leaf.key}
	}
	inter := makeCert(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Intermediate"},
		NotBefore:             notBefore,
		NotAfter:              s.intermediateNotAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil)
	leaf := makeCert(t, leafTmpl, &inter)
	return tls.Certificate{Certificate: [][]byte{leaf.der, inter.der}, PrivateKey: leaf.key}
}

// endpoint is a local TLS server; sni records each ServerName it was asked
// for and accepts counts connections.
type endpoint struct {
	addr     string
	accepts  atomic.Int32
	mu       sync.Mutex
	sni      []string
	resetOne bool // close the first connection before the handshake
	hangOne  bool // hold the first connection open and never answer
}

func serve(t *testing.T, spec certSpec, opts ...func(*endpoint)) *endpoint {
	t.Helper()
	e := &endpoint{}
	for _, o := range opts {
		o(e)
	}
	cert := spec.chain(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	e.addr = ln.Addr().String()
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			e.mu.Lock()
			e.sni = append(e.sni, hello.ServerName)
			e.mu.Unlock()
			return &cert, nil
		},
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if e.accepts.Add(1) == 1 {
				switch {
				case e.resetOne:
					_ = c.Close()
					continue
				case e.hangOne:
					t.Cleanup(func() { _ = c.Close() })
					continue
				}
			}
			go func(c net.Conn) {
				tc := tls.Server(c, cfg)
				_ = tc.Handshake()
				_ = tc.Close()
			}(c)
		}
	}()
	return e
}

func tlsEndpoint(t *testing.T, notAfter time.Time, names ...string) string {
	t.Helper()
	return serve(t, certSpec{names: names, notAfter: notAfter}).addr
}

// runWithCerts runs doctor with the real probe on a clock the test chooses,
// and returns the exposure findings.
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

func probeAddr(t *testing.T, addr string, pinned bool) CertProbe {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	return probeCertificate(context.Background(), CertTarget{Label: addr, Host: host, Port: port, Pinned: pinned})
}

func TestCertificateExpiredFails(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(-3*24*time.Hour-time.Hour), "127.0.0.1")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))

	if f.Severity != SeverityFail || !strings.Contains(f.Title, "expired 3 days ago") {
		t.Fatalf("an expired certificate did not fail: %+v", f)
	}
	if !strings.Contains(f.Detail, "Every client that checks certificates refuses") ||
		!strings.HasPrefix(f.Command, "openssl s_client -connect "+addr) || f.Runner != RunnerShell {
		t.Errorf("finding = %+v", f)
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

func TestCertificateJustOutsideTheWindowPasses(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(certExpiryWarning+time.Minute), "127.0.0.1")
	if f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now)); f.Severity != SeverityPass {
		t.Fatalf("got %+v", f)
	}
}

// A chain is refused when any certificate in it is out of date, so an
// intermediate that expires before the leaf is the date that matters.
func TestExpiredIntermediateFailsAValidLeaf(t *testing.T) {
	now := time.Now()
	e := serve(t, certSpec{names: []string{"127.0.0.1"}, notAfter: now.Add(80 * 24 * time.Hour), intermediateNotAfter: now.Add(-2 * 24 * time.Hour)})
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{e.addr}}, now))
	if f.Severity != SeverityFail || !strings.Contains(f.Title, "expired") || !strings.Contains(f.Detail, "Its intermediate Test Intermediate expired") {
		t.Fatalf("an expired intermediate was not failed: %+v", f)
	}
}

func TestNotYetValidCertificateFails(t *testing.T) {
	now := time.Now()
	e := serve(t, certSpec{names: []string{"127.0.0.1"}, notBefore: now.Add(3 * 24 * time.Hour), notAfter: now.Add(90 * 24 * time.Hour)})
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{e.addr}}, now))
	if f.Severity != SeverityFail || !strings.Contains(f.Title, "is not valid until") {
		t.Fatalf("a certificate from the future was not failed: %+v", f)
	}
}

func TestCertificateForAnotherNameFails(t *testing.T) {
	now := time.Now()
	addr := tlsEndpoint(t, now.Add(60*24*time.Hour), "other.example.test")
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{addr}}, now))
	if f.Severity != SeverityFail || !strings.Contains(f.Title, "does not name 127.0.0.1") || !strings.Contains(f.Detail, "It covers other.example.test") {
		t.Fatalf("a certificate for another name did not fail: %+v", f)
	}
}

// A certificate with only a common name names nothing to a modern client, and
// the finding has to say that rather than list the CN as a name it covers.
func TestCommonNameOnlyCertificateSaysTheCNIsIgnored(t *testing.T) {
	now := time.Now()
	e := serve(t, certSpec{cn: "127.0.0.1", notAfter: now.Add(60 * 24 * time.Hour)})
	f := onlyFinding(t, runWithCerts(t, &config.Config{Certificates: []string{e.addr}}, now))
	if f.Severity != SeverityFail || !strings.Contains(f.Detail, `no subject alternative names, only the common name "127.0.0.1"`) ||
		!strings.Contains(f.Detail, "modern clients ignore the common name") || strings.Contains(f.Detail, "It covers") {
		t.Fatalf("detail = %q", f.Detail)
	}
}

// Dialled by name, the probe sends that name as SNI and checks the
// certificate against it — the same as a browser reaching a reverse proxy.
func TestProbeSendsTheHostAsSNI(t *testing.T) {
	now := time.Now()
	e := serve(t, certSpec{names: []string{"home.example.test"}, notAfter: now.Add(60 * 24 * time.Hour)})
	_, port, _ := net.SplitHostPort(e.addr)
	old := certDial
	certDial = func(ctx context.Context, addr string) (net.Conn, error) {
		if addr != net.JoinHostPort("home.example.test", port) {
			t.Errorf("dialled %s", addr)
		}
		return old(ctx, e.addr)
	}
	t.Cleanup(func() { certDial = old })

	p := probeCertificate(context.Background(), CertTarget{Host: "home.example.test", Port: port})
	if p.Err != nil || p.NameErr != nil {
		t.Fatalf("probe = %+v", p)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.sni) != 1 || e.sni[0] != "home.example.test" {
		t.Fatalf("SNI sent = %v", e.sni)
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

// One reset is retried inside the same deadline.
func TestProbeRetriesOneReset(t *testing.T) {
	e := serve(t, certSpec{names: []string{"127.0.0.1"}, notAfter: time.Now().Add(60 * 24 * time.Hour)},
		func(e *endpoint) { e.resetOne = true })
	if p := probeAddr(t, e.addr, false); p.Err != nil {
		t.Fatalf("a single reset was not retried: %v", p.Err)
	}
	if n := e.accepts.Load(); n != 2 {
		t.Errorf("connections = %d, want 2", n)
	}
}

// A handshake the server silently drops gets half the deadline, so the retry
// still has time to read the certificate.
func TestProbeRetriesADroppedHandshake(t *testing.T) {
	e := serve(t, certSpec{names: []string{"127.0.0.1"}, notAfter: time.Now().Add(60 * 24 * time.Hour)},
		func(e *endpoint) { e.hangOne = true })
	host, port, _ := net.SplitHostPort(e.addr)
	ctx, cancel := context.WithTimeout(context.Background(), certDialTimeout)
	defer cancel()
	if p := probeCertificate(ctx, CertTarget{Host: host, Port: port}); p.Err != nil {
		t.Fatalf("a dropped first handshake used up the deadline: %v", p.Err)
	}
	if n := e.accepts.Load(); n != 2 {
		t.Errorf("connections = %d, want 2", n)
	}
}

// A port that accepts and never speaks TLS costs the deadline, not a hang,
// and the finding says so in words.
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
		if p.Err == nil || p.Err.Error() != "no answer within 5 seconds" {
			t.Fatalf("err = %v, want a sentence about the timeout", p.Err)
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

// Pinned follows the Proxmox client's own trust order: a fingerprint
// replaces every check, a ca_file verifies fully even beside insecure, and
// insecure alone verifies nothing.
func TestProxmoxEndpointsAreCheckedWithoutBeingListed(t *testing.T) {
	cfg := &config.Config{
		Certificates: []string{"home.example.test"},
		Proxmox: []config.ProxmoxConfig{
			{Name: "pinned", Host: "10.0.0.2", Fingerprint: strings.Repeat("ab", 32)},
			{Name: "trusted", Host: "pve.example.test", Port: 8443, CAFile: "/ca.pem"},
			{Name: "loose", Host: "10.0.0.3", Insecure: true},
			{Name: "ca-and-insecure", Host: "10.0.0.4", CAFile: "/ca.pem", Insecure: true},
		},
	}
	got := certTargets(cfg)
	want := []CertTarget{
		{Label: "home.example.test:443", Host: "home.example.test", Port: "443"},
		{Label: `Proxmox endpoint "pinned"`, Host: "10.0.0.2", Port: "8006", Proxmox: "pinned", Pinned: true},
		{Label: `Proxmox endpoint "trusted"`, Host: "pve.example.test", Port: "8443", Proxmox: "trusted"},
		{Label: `Proxmox endpoint "loose"`, Host: "10.0.0.3", Port: "8006", Proxmox: "loose", Pinned: true},
		{Label: `Proxmox endpoint "ca-and-insecure"`, Host: "10.0.0.4", Port: "8006", Proxmox: "ca-and-insecure"},
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

// homebutler keeps working against a pinned endpoint after its certificate
// expires, so the finding is a warning about other clients, and the name is
// not judged at all.
func TestPinnedCertificateExpiryIsAWarningAboutOtherClients(t *testing.T) {
	now := time.Now()
	p := probeAddr(t, tlsEndpoint(t, now.Add(60*24*time.Hour), "pve.internal"), true)
	if p.Err != nil || p.NameErr != nil {
		t.Fatalf("pinned endpoint probe = %+v", p)
	}

	expired := probeAddr(t, tlsEndpoint(t, now.Add(-time.Hour), "pve.internal"), true)
	r := &Result{}
	checkCertificates(r, []CertProbe{expired}, now, nil)
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityWarn ||
		!strings.Contains(r.Findings[0].Detail, "homebutler keeps working here, but a browser") {
		t.Fatalf("an expired pinned certificate = %+v", r.Findings)
	}
}

// An endpoint checkProxmox already failed is one finding, not two.
func TestUnreachableProxmoxIsNotReportedTwice(t *testing.T) {
	probe := CertProbe{Target: CertTarget{Label: `Proxmox endpoint "pve"`, Proxmox: "pve"}, Err: errNoAnswer}
	r := &Result{}
	checkCertificates(r, []CertProbe{probe}, time.Now(), map[string]bool{"pve": true})
	if len(r.Findings) != 0 {
		t.Fatalf("a Proxmox endpoint already reported unreachable got a second finding: %+v", r.Findings)
	}
	checkCertificates(r, []CertProbe{probe}, time.Now(), map[string]bool{})
	if len(r.Findings) != 1 || r.Findings[0].Detail != "No answer within 5 seconds." {
		t.Fatalf("a reachable Proxmox endpoint whose TLS failed was not reported: %+v", r.Findings)
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

// Results come back in config order however long each dial takes, the dials
// overlap, no more than certParallel run at once, and each has its own
// deadline of certDialTimeout.
func TestCertificateProbesRunInParallelWithinTheCap(t *testing.T) {
	var targets []CertTarget
	for i := 0; i < 20; i++ {
		targets = append(targets, CertTarget{Label: string(rune('a' + i))})
	}
	var running, most atomic.Int32
	start := time.Now()
	got := probeCertificates(targets, func(ctx context.Context, target CertTarget) CertProbe {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > certDialTimeout || time.Until(deadline) < certDialTimeout-time.Second {
			t.Errorf("probe %s has deadline %v (ok=%v), want its own %s", target.Label, time.Until(deadline), ok, certDialTimeout)
		}
		time.Sleep(time.Duration(20-int(target.Label[0]-'a')) * 5 * time.Millisecond)
		running.Add(-1)
		return CertProbe{Target: target}
	})
	for i, p := range got {
		if p.Target.Label != targets[i].Label {
			t.Fatalf("result %d is %q, want %q", i, p.Target.Label, targets[i].Label)
		}
	}
	if m := most.Load(); m > certParallel || m < 2 {
		t.Errorf("at most %d ran at once; want between 2 and %d", m, certParallel)
	}
	// Twenty sequential dials would take 1.05s.
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Errorf("20 probes took %s; they are not overlapping", elapsed)
	}
}
