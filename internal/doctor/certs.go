package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
)

// certDialTimeout bounds the connect and the handshake together. doctor is
// run when something is already wrong, and an endpoint that does not answer
// should cost a few seconds, not the Proxmox client's ten.
const certDialTimeout = 5 * time.Second

// certExpiryWarning is how close to expiry a certificate is worth a warning.
// ACME clients renew at around 30 days left, so a certificate inside 14 has
// usually missed more than one attempt already.
const certExpiryWarning = 14 * 24 * time.Hour

// certParallel caps how many endpoints are dialled at once.
const certParallel = 8

// certRetryPause is the wait before the one retry. Resets from a server that
// is shedding connections tend to come in a burst, and an immediate retry
// lands in the same one.
const certRetryPause = 300 * time.Millisecond

// CertTarget is one endpoint whose certificate doctor reads.
type CertTarget struct {
	// Label is what a finding calls it: host:port for a listed endpoint, the
	// endpoint name for Proxmox.
	Label string
	Host  string
	Port  string
	// Proxmox is the endpoint name when this target is a Proxmox endpoint.
	Proxmox string
	// Pinned is true where homebutler's own client does not verify the
	// certificate at all — a Proxmox fingerprint pin, or insecure: true with
	// no ca_file. Neither the name nor the expiry matters to homebutler
	// there; the expiry still matters to a browser.
	Pinned bool
	// Invalid carries a config entry that could not be parsed.
	Invalid error
}

// CertProbe is what reading one endpoint's certificate found.
type CertProbe struct {
	Target CertTarget
	// NotAfter is the earliest end date in the presented chain, and Expiring
	// says whose it is: "" for the leaf, or an intermediate's subject.
	NotAfter time.Time
	Expiring string
	// NotBefore is the latest start date in the chain, and Starting whose.
	NotBefore time.Time
	Starting  string
	Subject   string
	Issuer    string
	// Names are the DNS and IP subject alternative names of the leaf.
	Names []string
	// NameErr is set when the certificate does not cover Target.Host.
	NameErr error
	// Err is set when no certificate was read at all.
	Err error
}

// certTargets lists the endpoints to check: every certificates entry, then
// every Proxmox endpoint. No list and no Proxmox means nothing is checked and
// nothing is reported.
func certTargets(cfg *config.Config) []CertTarget {
	if cfg == nil {
		return nil
	}
	var out []CertTarget
	for _, entry := range cfg.Certificates {
		host, port, err := config.CertificateEndpoint(entry)
		label := entry
		if err == nil {
			label = net.JoinHostPort(host, port)
		}
		out = append(out, CertTarget{Label: label, Host: host, Port: port, Invalid: err})
	}
	for _, p := range cfg.Proxmox {
		if p.Host == "" {
			continue // checkProxmox already reports an endpoint with no host
		}
		out = append(out, CertTarget{
			Label:   fmt.Sprintf("Proxmox endpoint %q", p.Name),
			Host:    p.Host,
			Port:    strconv.Itoa(p.APIPort()),
			Proxmox: p.Name,
			// The client's own trust order: a fingerprint replaces every
			// other check, a ca_file verifies fully and wins over insecure,
			// and insecure alone verifies nothing.
			Pinned: p.Fingerprint != "" || (p.CAFile == "" && p.Insecure),
		})
	}
	return out
}

// probeCertificates reads every target, at most certParallel at a time, each
// with its own certDialTimeout, and returns the results in target order.
func probeCertificates(targets []CertTarget, probe func(context.Context, CertTarget) CertProbe) []CertProbe {
	results := make([]CertProbe, len(targets))
	sem := make(chan struct{}, certParallel)
	var wg sync.WaitGroup
	for i, target := range targets {
		if target.Invalid != nil {
			results[i] = CertProbe{Target: target, Err: target.Invalid}
			continue
		}
		wg.Add(1)
		go func(i int, target CertTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), certDialTimeout)
			defer cancel()
			results[i] = probe(ctx, target)
		}(i, target)
	}
	wg.Wait()
	return results
}

// certDial opens the TCP connection. A variable so a test can serve a
// certificate for a name on 127.0.0.1 and still dial by that name.
var certDial = func(ctx context.Context, addr string) (net.Conn, error) {
	return (&net.Dialer{Timeout: certDialTimeout}).DialContext(ctx, "tcp", addr)
}

func handshake(ctx context.Context, addr string, config *tls.Config) (*tls.Conn, error) {
	raw, err := certDial(ctx, addr)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return conn, nil
}

// probeCertificate connects to target and reads the certificate chain it
// presents.
//
// Chain verification is skipped on purpose: the question is when this
// certificate stops working and whether it names the host, and a self-signed
// Proxmox certificate pinned by fingerprint is a certificate that works. The
// name is checked by hand, against the same host the connection was made to.
func probeCertificate(ctx context.Context, target CertTarget) CertProbe {
	result := CertProbe{Target: target}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // #nosec G402 -- reads the certificate, trusts nothing it says.
		MinVersion:         tls.VersionTLS12,
	}
	if net.ParseIP(target.Host) == nil {
		tlsConfig.ServerName = target.Host
	}
	addr := net.JoinHostPort(target.Host, target.Port)
	// The first attempt gets half the deadline, so a handshake the server
	// silently drops still leaves time for the retry. Both a reset and a
	// dropped handshake happen often enough on a real network that warning on
	// one would teach people to ignore the warning.
	first, cancel := context.WithTimeout(ctx, certDialTimeout/2)
	conn, err := handshake(first, addr, tlsConfig)
	cancel()
	if err != nil && ctx.Err() == nil {
		select {
		case <-time.After(certRetryPause):
		case <-ctx.Done():
		}
		conn, err = handshake(ctx, addr, tlsConfig)
	}
	if err != nil {
		result.Err = describeDialError(ctx, err)
		return result
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		result.Err = errors.New("the server sent no certificate")
		return result
	}
	leaf := certs[0]
	result.Subject = leaf.Subject.CommonName
	result.Issuer = leaf.Issuer.CommonName
	result.Names = certNames(leaf)
	// A client refuses the chain when any certificate in it is out of its
	// window, so the window that matters is the narrowest one.
	for i, c := range certs {
		who := ""
		if i > 0 {
			who = "intermediate " + c.Subject.CommonName
		}
		if result.NotAfter.IsZero() || c.NotAfter.Before(result.NotAfter) {
			result.NotAfter, result.Expiring = c.NotAfter, who
		}
		if c.NotBefore.After(result.NotBefore) {
			result.NotBefore, result.Starting = c.NotBefore, who
		}
	}
	if !target.Pinned {
		result.NameErr = leaf.VerifyHostname(target.Host)
	}
	return result
}

// errNoAnswer is what a probe that ran out of time reports.
var errNoAnswer = fmt.Errorf("no answer within %d seconds", int(certDialTimeout.Seconds()))

// describeDialError turns a timeout into a sentence; "context deadline
// exceeded" says what Go gave up on, not what the endpoint did.
func describeDialError(ctx context.Context, err error) error {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) || ctx.Err() != nil {
		return errNoAnswer
	}
	return err
}

func certNames(leaf *x509.Certificate) []string {
	names := append([]string(nil), leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	return names
}

// opensslCommand is the command that shows the same certificate by hand.
func opensslCommand(target CertTarget) string {
	addr := net.JoinHostPort(target.Host, target.Port)
	sni := ""
	if net.ParseIP(target.Host) == nil {
		sni = " -servername " + target.Host
	}
	return "openssl s_client -connect " + addr + sni + " </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer -dates"
}

// dialDetail is a failed probe's error as a finding's detail.
func dialDetail(err error) string {
	if errors.Is(err, errNoAnswer) {
		return "No answer within " + strconv.Itoa(int(certDialTimeout.Seconds())) + " seconds."
	}
	return err.Error()
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
