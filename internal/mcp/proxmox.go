package mcp

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Higangssh/homebutler/internal/proxmox"
)

func proxmoxEndpointProperties() map[string]propDef {
	return map[string]propDef{
		"endpoint": {Type: "string", Description: "Proxmox endpoint name from config (optional when exactly one is configured)"},
	}
}

func proxmoxGuestActionSchema() inputSchema {
	return inputSchema{Type: "object", Properties: map[string]propDef{
		"endpoint": {Type: "string", Description: "Explicit Proxmox endpoint name from config"},
		"node":     {Type: "string", Description: "Proxmox node name"},
		"type":     {Type: "string", Description: "Guest type: qemu or lxc"},
		"vmid":     {Type: "integer", Description: "Proxmox guest VMID from 1 through 999999999"},
		"confirm":  {Type: "boolean", Description: "Must be true to confirm the explicit guest action target"},
	}, Required: []string{"endpoint", "node", "type", "vmid", "confirm"}}
}

type proxmoxGuestActionRequest struct {
	Endpoint string
	Node     string
	Type     string
	VMID     int
	Action   proxmox.GuestAction
}

type proxmoxGuestActionResult struct {
	Endpoint string              `json:"endpoint"`
	Node     string              `json:"node"`
	Type     string              `json:"type"`
	VMID     int                 `json:"vmid"`
	Action   proxmox.GuestAction `json:"action"`
	Status   string              `json:"status"`
	UPID     string              `json:"upid"`
}

func (s *Server) executeProxmox(name string, args map[string]any) (any, error) {
	actionRequest, isAction, err := proxmoxGuestActionArgs(name, args)
	if err != nil {
		return nil, err
	}
	var taskStatusEndpoint, taskStatusNode, taskStatusUPID string
	if name == "proxmox_task_status" {
		values := make(map[string]string, 3)
		for _, key := range []string{"endpoint", "node", "upid"} {
			value, err := strictProxmoxStringArg(args, key)
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
		taskStatusEndpoint, taskStatusNode, taskStatusUPID = values["endpoint"], values["node"], values["upid"]
		if err := proxmox.ValidateTaskStatusRequest(taskStatusNode, taskStatusUPID); err != nil {
			return nil, err
		}
	}
	if (name == "proxmox_node" || name == "proxmox_tasks") && stringArg(args, "node") == "" {
		return nil, fmt.Errorf("missing required parameter: node")
	}
	endpointName := stringArg(args, "endpoint")
	if isAction {
		endpointName = actionRequest.Endpoint
	} else if name == "proxmox_task_status" {
		endpointName = taskStatusEndpoint
	}
	endpoint, err := s.cfg.SelectProxmox(endpointName)
	if err != nil {
		return nil, err
	}
	tokenID, token, err := endpoint.ResolveCredential(isAction)
	if err != nil {
		return nil, err
	}
	client, err := proxmox.New(proxmox.Options{
		Host: endpoint.Host, Port: endpoint.APIPort(), TokenID: tokenID, Token: token,
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
	case "proxmox_guest_start", "proxmox_guest_reboot", "proxmox_guest_shutdown":
		upid, err := client.ActOnGuest(ctx, actionRequest.Node, actionRequest.Type, actionRequest.VMID, actionRequest.Action)
		if err != nil {
			return nil, err
		}
		return proxmoxGuestActionResult{
			Endpoint: endpoint.Name, Node: actionRequest.Node, Type: actionRequest.Type, VMID: actionRequest.VMID,
			Action: actionRequest.Action, Status: "accepted", UPID: upid,
		}, nil
	case "proxmox_task_status":
		return client.TaskStatus(ctx, taskStatusNode, taskStatusUPID)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func proxmoxGuestActionArgs(name string, args map[string]any) (proxmoxGuestActionRequest, bool, error) {
	var action proxmox.GuestAction
	switch name {
	case "proxmox_guest_start":
		action = proxmox.GuestActionStart
	case "proxmox_guest_reboot":
		action = proxmox.GuestActionReboot
	case "proxmox_guest_shutdown":
		action = proxmox.GuestActionShutdown
	default:
		return proxmoxGuestActionRequest{}, false, nil
	}

	endpoint, err := strictProxmoxStringArg(args, "endpoint")
	if err != nil {
		return proxmoxGuestActionRequest{}, true, err
	}
	node, err := strictProxmoxStringArg(args, "node")
	if err != nil {
		return proxmoxGuestActionRequest{}, true, err
	}
	guestType, err := strictProxmoxStringArg(args, "type")
	if err != nil {
		return proxmoxGuestActionRequest{}, true, err
	}
	vmid, err := strictProxmoxVMID(args)
	if err != nil {
		return proxmoxGuestActionRequest{}, true, err
	}
	if err := proxmox.ValidateGuestAction(node, guestType, vmid, action); err != nil {
		return proxmoxGuestActionRequest{}, true, err
	}
	confirmed, ok := args["confirm"].(bool)
	if !ok || !confirmed {
		return proxmoxGuestActionRequest{}, true, fmt.Errorf("confirmation required for Proxmox guest action: endpoint=%q node=%q type=%q vmid=%d action=%q; set confirm to true", endpoint, node, guestType, vmid, action)
	}
	return proxmoxGuestActionRequest{Endpoint: endpoint, Node: node, Type: guestType, VMID: vmid, Action: action}, true, nil
}

func strictProxmoxStringArg(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("missing or invalid required parameter: %s", key)
	}
	return value, nil
}

func strictProxmoxVMID(args map[string]any) (int, error) {
	var value int
	switch raw := args["vmid"].(type) {
	case int:
		value = raw
	case float64:
		if math.IsNaN(raw) || math.IsInf(raw, 0) || raw != math.Trunc(raw) || raw < 0 || raw > math.MaxInt {
			return 0, fmt.Errorf("missing or invalid required parameter: vmid")
		}
		value = int(raw)
	default:
		return 0, fmt.Errorf("missing or invalid required parameter: vmid")
	}
	return value, nil
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
