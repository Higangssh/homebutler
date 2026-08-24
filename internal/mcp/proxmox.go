package mcp

import (
	"context"
	"fmt"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/proxmox"
)

func proxmoxEndpointProperties() map[string]propDef {
	return map[string]propDef{
		"endpoint": {Type: "string", Description: "Proxmox endpoint name from config (optional when exactly one is configured)"},
	}
}

func (s *Server) executeProxmox(name string, args map[string]any) (any, error) {
	if (name == "proxmox_node" || name == "proxmox_tasks") && stringArg(args, "node") == "" {
		return nil, fmt.Errorf("missing required parameter: node")
	}
	endpoint, err := selectProxmoxEndpoint(s.cfg, stringArg(args, "endpoint"))
	if err != nil {
		return nil, err
	}
	token, err := endpoint.TokenValue()
	if err != nil {
		return nil, fmt.Errorf("read token for Proxmox endpoint %q: %w", endpoint.Name, err)
	}
	client, err := proxmox.New(proxmox.Options{
		Host: endpoint.Host, Port: endpoint.APIPort(), TokenID: endpoint.TokenID, Token: token,
		Fingerprint: endpoint.Fingerprint, CAFile: endpoint.CAFile, Insecure: endpoint.Insecure, Timeout: endpoint.TimeoutDuration(),
	})
	if err != nil {
		return nil, fmt.Errorf("configure Proxmox endpoint %q: %w", endpoint.Name, err)
	}

	ctx := context.Background()
	switch name {
	case "proxmox_status":
		return client.DefaultView(ctx)
	case "proxmox_guests":
		resources, err := client.Resources(ctx)
		if err != nil {
			return nil, err
		}
		return filterProxmoxGuests(resources.Guests, stringArg(args, "node"), stringArg(args, "status"), stringArg(args, "type")), nil
	case "proxmox_node":
		return client.NodeStatus(ctx, stringArg(args, "node"))
	case "proxmox_tasks":
		return client.Tasks(ctx, stringArg(args, "node"))
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func selectProxmoxEndpoint(c *config.Config, name string) (*config.ProxmoxConfig, error) {
	if name != "" {
		endpoint := c.FindProxmox(name)
		if endpoint == nil {
			return nil, fmt.Errorf("proxmox endpoint %q not found in config", name)
		}
		return endpoint, nil
	}
	if len(c.Proxmox) == 0 {
		return nil, fmt.Errorf("no Proxmox endpoints configured")
	}
	if len(c.Proxmox) > 1 {
		return nil, fmt.Errorf("multiple Proxmox endpoints configured; use endpoint")
	}
	return &c.Proxmox[0], nil
}

func filterProxmoxGuests(guests []proxmox.Guest, node, status, kind string) []proxmox.Guest {
	filtered := make([]proxmox.Guest, 0, len(guests))
	for _, guest := range guests {
		if (node == "" || guest.Node == node) && (status == "" || guest.Status == status) && (kind == "" || guest.Type == kind) {
			filtered = append(filtered, guest)
		}
	}
	return filtered
}
