// Package proxmox provides the small Proxmox VE API client used by
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
	"errors"
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

// FailureClass describes an actionable Proxmox endpoint failure.
// An empty class means the error is not an endpoint failure, or there was none.
type FailureClass string

const (
	FailureTLS            FailureClass = "tls"
	FailureAuthentication FailureClass = "authentication"
	FailureAuthorization  FailureClass = "authorization"
	FailureTransport      FailureClass = "transport"
)

type classifiedError struct {
	class FailureClass
	err   error
}

func (e *classifiedError) Error() string { return e.err.Error() }
func (e *classifiedError) Unwrap() error { return e.err }

// Classify returns the failure class carried by err, if any.
func Classify(err error) FailureClass {
	var classified *classifiedError
	if errors.As(err, &classified) {
		return classified.class
	}
	return ""
}

// WithFailureClass carries class outward without changing err's message.
func WithFailureClass(class FailureClass, err error) error {
	if err == nil || class == "" || Classify(err) != "" {
		return err
	}
	return &classifiedError{class: class, err: err}
}

const (
	// minVMID matches the Proxmox API's own vmid schema minimum. 100 is only
	// the default lower bound of /cluster/nextid's auto-assign range
	// (datacenter.cfg next-id), not a floor the API enforces on an
	// explicitly supplied vmid, and a cluster or an older pvesh-created
	// guest can legitimately sit below it.
	minVMID = 1
	maxVMID = 999999999
)

var guestActionPaths = map[GuestAction]string{
	GuestActionStart:    "start",
	GuestActionShutdown: "shutdown",
	GuestActionReboot:   "reboot",
}

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

// Client is a client for the Proxmox VE JSON API.
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
		return nil, WithFailureClass(FailureTLS, err)
	}

	return &Client{
		baseURL: "https://" + net.JoinHostPort(options.Host, fmt.Sprint(options.Port)),
		tokenID: options.TokenID,
		token:   options.Token,
		http: &http.Client{
			Timeout:   options.Timeout,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) > 0 && via[0].Method == http.MethodPost {
					return http.ErrUseLastResponse
				}
				return nil
			},
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
				return WithFailureClass(FailureTLS, fmt.Errorf("proxmox TLS peer sent no certificate"))
			}
			if _, err := x509.ParseCertificate(rawCerts[0]); err != nil {
				return fmt.Errorf("parse Proxmox TLS certificate: %w", err)
			}
			actual := sha256.Sum256(rawCerts[0])
			if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
				return WithFailureClass(FailureTLS, fmt.Errorf("proxmox TLS certificate fingerprint mismatch"))
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

// ActOnGuest submits one approved guest power action and returns its opaque UPID.
func (c *Client) ActOnGuest(ctx context.Context, node, guestType string, vmid int, action GuestAction) (string, error) {
	if err := ValidateGuestAction(node, guestType, vmid, action); err != nil {
		return "", err
	}
	suffix := guestActionPaths[action]
	path := fmt.Sprintf("/api2/json/nodes/%s/%s/%d/status/%s",
		url.PathEscape(node), url.PathEscape(guestType), vmid, url.PathEscape(suffix))
	var upid string
	if err := c.post(ctx, path, &upid); err != nil {
		return "", err
	}
	if strings.TrimSpace(upid) == "" {
		return "", fmt.Errorf("proxmox guest action returned an empty UPID")
	}
	return upid, nil
}

// ValidateGuestAction validates an explicit guest action target without making a request.
func ValidateGuestAction(node, guestType string, vmid int, action GuestAction) error {
	if strings.TrimSpace(node) == "" {
		return fmt.Errorf("proxmox node is required")
	}
	if guestType != "qemu" && guestType != "lxc" {
		return fmt.Errorf("proxmox guest type must be qemu or lxc")
	}
	if vmid < minVMID || vmid > maxVMID {
		return fmt.Errorf("proxmox VMID must be between %d and %d", minVMID, maxVMID)
	}
	_, ok := guestActionPaths[action]
	if !ok {
		return fmt.Errorf("unsupported Proxmox guest action %q", action)
	}
	return nil
}

// TaskStatus returns the current status of one opaque Proxmox task UPID.
func (c *Client) TaskStatus(ctx context.Context, node, upid string) (TaskStatus, error) {
	if err := ValidateTaskStatusRequest(node, upid); err != nil {
		return TaskStatus{}, err
	}
	var status TaskStatus
	path := "/api2/json/nodes/" + url.PathEscape(node) + "/tasks/" + url.PathEscape(upid) + "/status"
	if err := c.get(ctx, path, nil, &status); err != nil {
		return TaskStatus{}, err
	}
	status.Result = TaskResult(status.Status, status.ExitStatus)
	return status, nil
}

// ValidateTaskStatusRequest validates a task lookup without making a request.
func ValidateTaskStatusRequest(node, upid string) error {
	if strings.TrimSpace(node) == "" {
		return fmt.Errorf("proxmox node is required")
	}
	if strings.TrimSpace(upid) == "" {
		return fmt.Errorf("proxmox UPID is required")
	}
	return nil
}

// TaskResult classifies the asynchronous task state as a short, stable token.
func TaskResult(status, exitStatus string) string {
	if status == "running" {
		return "running"
	}
	if status == "stopped" && exitStatus == "OK" {
		return "ok"
	}
	if status == "stopped" {
		return "failed"
	}
	return "unknown"
}

// DefaultView performs the three requests used by the default status view.
func (c *Client) DefaultView(ctx context.Context) (DefaultView, error) {
	view := DefaultView{}
	if version, err := c.Version(ctx); err != nil {
		view.Warnings = append(view.Warnings, "version: "+err.Error())
		view.Failed = append(view.Failed, CollectorVersion)
	} else {
		view.Version = version
	}
	if cluster, err := c.ClusterStatus(ctx); err != nil {
		view.Warnings = append(view.Warnings, "cluster: "+err.Error())
		view.Failed = append(view.Failed, CollectorCluster)
	} else {
		view.Cluster = cluster
	}
	if resources, err := c.Resources(ctx); err != nil {
		view.Warnings = append(view.Warnings, "resources: "+err.Error())
		view.Failed = append(view.Failed, CollectorResources)
	} else if len(resources.Nodes) == 0 && len(resources.Guests) == 0 && len(resources.Storage) == 0 {
		view.Warnings = append(view.Warnings, "resources: no resources visible; check Proxmox token permissions")
		view.Failed = append(view.Failed, CollectorResources)
	} else {
		view.Resources = resources
	}
	return view, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.request(ctx, http.MethodGet, path, query, out)
}

func (c *Client) post(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodPost, path, nil, out)
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, out any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("build Proxmox request URL: %w", err)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return fmt.Errorf("build Proxmox request: %w", err)
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		class := FailureTransport
		if Classify(err) == FailureTLS || isTLSFailure(err) {
			class = FailureTLS
		}
		return WithFailureClass(class, fmt.Errorf("proxmox request %s: %w", path, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return WithFailureClass(FailureTransport, fmt.Errorf("read Proxmox response %s: %w", path, err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var apiError struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiError)
		if message := strings.TrimSpace(apiError.Message); message != "" {
			message = strings.ReplaceAll(message, c.token, "[REDACTED]")
			return WithFailureClass(httpFailureClass(resp.StatusCode), fmt.Errorf("proxmox request %s: %s: %s", path, resp.Status, message))
		}
		return WithFailureClass(httpFailureClass(resp.StatusCode), fmt.Errorf("proxmox request %s: %s", path, resp.Status))
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

func httpFailureClass(statusCode int) FailureClass {
	switch statusCode {
	case http.StatusUnauthorized:
		return FailureAuthentication
	case http.StatusForbidden:
		return FailureAuthorization
	default:
		return ""
	}
}

func isTLSFailure(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalidCertificate) || errors.As(err, &verification)
}
