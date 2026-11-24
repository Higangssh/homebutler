package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
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

// CertTarget is one endpoint whose certificate doctor reads.
type CertTarget struct {
	// Label is what a finding calls it: host:port for a listed endpoint, the
	// endpoint name for Proxmox.
	Label string
	Host  string
	Port  string
	// CheckName is false where the client that uses this endpoint has
	// deliberately replaced hostname verification, as a Proxmox fingerprint
	// pin or insecure: true does. Failing a name that nothing checks would
	// be a finding about a problem nobody has.
	CheckName bool
	// Invalid carries a config entry that could not be parsed.
	Invalid error
}

// CertProbe is what reading one endpoint's certificate found.
type CertProbe struct {
	Target   CertTarget
	NotAfter time.Time
	Subject  string
	Issuer   string
	// Names are the DNS and IP names the leaf certificate covers.
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
		out = append(out, CertTarget{Label: label, Host: host, Port: port, CheckName: true, Invalid: err})
	}
	for _, p := range cfg.Proxmox {
		if p.Host == "" {
			continue // checkProxmox already reports an endpoint with no host
		}
		out = append(out, CertTarget{
			Label:     fmt.Sprintf("Proxmox endpoint %q", p.Name),
			Host:      p.Host,
			Port:      strconv.Itoa(p.APIPort()),
			CheckName: p.Fingerprint == "" && !p.Insecure,
		})
	}
	return out
}

// probeCertificates reads every target, at most certParallel at a time, and
// returns the results in the order the targets were given.
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

// probeCertificate connects to target and reads the leaf certificate.
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
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: certDialTimeout}, Config: tlsConfig}
	addr := net.JoinHostPort(target.Host, target.Port)
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil && ctx.Err() == nil {
		// One more try inside the same deadline. A single reset is common
		// enough on a real network that warning on it would teach people to
		// ignore the warning; a timeout is not retried, since the deadline
		// is already spent.
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		result.Err = err
		return result
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		result.Err = errors.New("not a TLS connection")
		return result
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		result.Err = errors.New("the server sent no certificate")
		return result
	}
	leaf := certs[0]
	result.NotAfter = leaf.NotAfter
	result.Subject = leaf.Subject.CommonName
	result.Issuer = leaf.Issuer.CommonName
	result.Names = certNames(leaf)
	if target.CheckName {
		result.NameErr = leaf.VerifyHostname(target.Host)
	}
	return result
}

func certNames(leaf *x509.Certificate) []string {
	names := append([]string(nil), leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	if len(names) == 0 && leaf.Subject.CommonName != "" {
		names = append(names, leaf.Subject.CommonName)
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
