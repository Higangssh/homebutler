// Package proxmox provides the small read-only Proxmox VE API client used by
// HomeButler's Proxmox commands.
package proxmox

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

// Options configures a Proxmox API client. Token is the resolved token value;
// resolving token files belongs to the configuration layer.
type Options struct {
	Host        string
	Port        int
	TokenID     string
	Token       string
	Fingerprint string
	CAFile      string
	Insecure    bool
	Timeout     time.Duration
}

// Client is a read-only client for the Proxmox VE JSON API.
type Client struct {
	baseURL string
	tokenID string
	token   string
	http    *http.Client
}

// New returns a client configured for one Proxmox endpoint.
func New(options Options) (*Client, error) {
	if options.Host == "" {
		return nil, fmt.Errorf("proxmox host is required")
	}
	if options.TokenID == "" || options.Token == "" {
		return nil, fmt.Errorf("proxmox token ID and token are required")
	}
	if options.Port == 0 {
		options.Port = 8006
	}
	if options.Port < 1 || options.Port > 65535 {
		return nil, fmt.Errorf("invalid Proxmox port %d", options.Port)
	}
	if options.Timeout == 0 {
		options.Timeout = defaultTimeout
	}
	if options.Timeout < 0 {
		return nil, fmt.Errorf("invalid Proxmox timeout %s", options.Timeout)
	}

	tlsConfig, err := tlsConfig(options)
	if err != nil {
		return nil, err
	}

	return &Client{
		baseURL: "https://" + net.JoinHostPort(options.Host, fmt.Sprint(options.Port)),
		tokenID: options.TokenID,
		token:   options.Token,
		http: &http.Client{
			Timeout:   options.Timeout,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
	}, nil
}

func tlsConfig(options Options) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}

	if options.Fingerprint != "" {
		expected, err := parseFingerprint(options.Fingerprint)
		if err != nil {
			return nil, err
		}
		// Pinning deliberately replaces chain and hostname verification. Proxmox
		// commonly uses a self-signed certificate, so the standard verifier would
		// reject it before VerifyPeerCertificate gets the leaf certificate.
		config.InsecureSkipVerify = true // #nosec G402 -- verified by the callback below.
		config.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("proxmox TLS peer sent no certificate")
			}
			if _, err := x509.ParseCertificate(rawCerts[0]); err != nil {
				return fmt.Errorf("parse Proxmox TLS certificate: %w", err)
			}
			actual := sha256.Sum256(rawCerts[0])
			if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
				return fmt.Errorf("proxmox TLS certificate fingerprint mismatch")
			}
			return nil
		}
		return config, nil
	}

	if options.CAFile != "" {
		pem, err := os.ReadFile(options.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read Proxmox CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("proxmox CA file contains no certificates")
		}
		config.RootCAs = pool
		return config, nil
	}

	if options.Insecure {
		config.InsecureSkipVerify = true // #nosec G402 -- explicitly requested unsafe fallback.
	}
	return config, nil
}

func parseFingerprint(value string) ([]byte, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ":", "")
	fingerprint, err := hex.DecodeString(value)
	if err != nil || len(fingerprint) != sha256.Size {
		return nil, fmt.Errorf("proxmox fingerprint must be a SHA-256 certificate fingerprint")
	}
	return fingerprint, nil
}

// Version returns the Proxmox version and release information.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var version Version
	return version, c.get(ctx, "/api2/json/version", nil, &version)
}

// ClusterStatus returns cluster and node membership information.
func (c *Client) ClusterStatus(ctx context.Context) ([]ClusterStatus, error) {
	var status []ClusterStatus
	return status, c.get(ctx, "/api2/json/cluster/status", nil, &status)
}

// Resources returns nodes, guests, and storage from the cluster resources API.
func (c *Client) Resources(ctx context.Context) (Resources, error) {
	var raw []json.RawMessage
	if err := c.get(ctx, "/api2/json/cluster/resources", nil, &raw); err != nil {
		return Resources{}, err
	}

	resources := Resources{}
	for _, item := range raw {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item, &kind); err != nil {
			return Resources{}, fmt.Errorf("decode Proxmox resource type: %w", err)
		}
		switch kind.Type {
		case "node":
			var node Node
			if err := json.Unmarshal(item, &node); err != nil {
				return Resources{}, fmt.Errorf("decode Proxmox node: %w", err)
			}
			resources.Nodes = append(resources.Nodes, node)
		case "qemu", "lxc":
			var guest Guest
			if err := json.Unmarshal(item, &guest); err != nil {
				return Resources{}, fmt.Errorf("decode Proxmox guest: %w", err)
			}
			resources.Guests = append(resources.Guests, guest)
		case "storage":
			var store Store
			if err := json.Unmarshal(item, &store); err != nil {
				return Resources{}, fmt.Errorf("decode Proxmox storage: %w", err)
			}
			resources.Storage = append(resources.Storage, store)
		}
	}
	return resources, nil
}

// NodeStatus returns detailed status for node.
func (c *Client) NodeStatus(ctx context.Context, node string) (NodeStatus, error) {
	var status NodeStatus
	return status, c.get(ctx, "/api2/json/nodes/"+url.PathEscape(node)+"/status", nil, &status)
}

// Tasks returns the latest tasks for node, including running tasks.
func (c *Client) Tasks(ctx context.Context, node string) ([]Task, error) {
	return c.TasksLimit(ctx, node, 50)
}

// TasksLimit returns the latest tasks for node, including running tasks.
func (c *Client) TasksLimit(ctx context.Context, node string, limit int) ([]Task, error) {
	if limit < 1 {
		return nil, fmt.Errorf("proxmox task limit must be positive")
	}
	var tasks []Task
	query := url.Values{"source": {"all"}, "limit": {fmt.Sprint(limit)}}
	return tasks, c.get(ctx, "/api2/json/nodes/"+url.PathEscape(node)+"/tasks", query, &tasks)
}

// DefaultView performs the three requests used by the default status view.
func (c *Client) DefaultView(ctx context.Context) (DefaultView, error) {
	view := DefaultView{}
	if version, err := c.Version(ctx); err != nil {
		view.Warnings = append(view.Warnings, "version: "+err.Error())
		view.Failed = append(view.Failed, "version")
	} else {
		view.Version = version
	}
	if cluster, err := c.ClusterStatus(ctx); err != nil {
		view.Warnings = append(view.Warnings, "cluster: "+err.Error())
		view.Failed = append(view.Failed, "cluster")
	} else {
		view.Cluster = cluster
	}
	if resources, err := c.Resources(ctx); err != nil {
		view.Warnings = append(view.Warnings, "resources: "+err.Error())
		view.Failed = append(view.Failed, "resources")
	} else {
		view.Resources = resources
	}
	return view, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("build Proxmox request URL: %w", err)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("build Proxmox request: %w", err)
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("proxmox request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Proxmox response %s: %w", path, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var apiError struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiError)
		if message := strings.TrimSpace(apiError.Message); message != "" {
			return fmt.Errorf("proxmox request %s: %s: %s", path, resp.Status, message)
		}
		return fmt.Errorf("proxmox request %s: %s", path, resp.Status)
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode Proxmox response %s: %w", path, err)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode Proxmox data %s: %w", path, err)
	}
	return nil
}
