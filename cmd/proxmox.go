package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/proxmox"
	"github.com/spf13/cobra"
)

func newProxmoxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "proxmox",
		Short:        "Inspect Proxmox VE endpoints",
		SilenceUsage: true,
	}
	cmd.AddCommand(newProxmoxStatusCmd(), newProxmoxGuestsCmd(), newProxmoxNodeCmd(), newProxmoxTasksCmd(), newProxmoxGuestCmd(), newProxmoxTaskCmd(), newProxmoxScriptCmd())
	return cmd
}

func newProxmoxStatusCmd() *cobra.Command {
	var endpointName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Proxmox VE status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint, client, err := openProxmoxClient(endpointName)
			if err != nil {
				return err
			}
			view, err := client.DefaultView(context.Background())
			if err != nil {
				return fmt.Errorf("get Proxmox status from %q: %w", endpoint.Name, err)
			}
			return writeProxmoxStatus(cmd, endpoint.Name, view, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name")
	return cmd
}

func openProxmoxClient(endpointName string) (*config.ProxmoxConfig, *proxmox.Client, error) {
	if serverName != "" || allServers {
		return nil, nil, fmt.Errorf("proxmox commands do not support --server or --all; use --endpoint")
	}
	if err := loadConfig(); err != nil {
		return nil, nil, err
	}
	endpoint, err := cfg.SelectProxmox(endpointName)
	if err != nil {
		return nil, nil, err
	}
	token, err := endpoint.TokenValue()
	if err != nil {
		return nil, nil, fmt.Errorf("read token for Proxmox endpoint %q: %w", endpoint.Name, err)
	}
	client, err := proxmox.New(proxmox.Options{
		Host: endpoint.Host, Port: endpoint.APIPort(), TokenID: endpoint.TokenID, Token: token,
		Fingerprint: endpoint.Fingerprint, CAFile: endpoint.CAFile, Insecure: endpoint.Insecure, Timeout: endpoint.TimeoutDuration(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure Proxmox endpoint %q: %w", endpoint.Name, err)
	}
	return endpoint, client, nil
}

func newProxmoxGuestsCmd() *cobra.Command {
	var endpointName, node, status string
	cmd := &cobra.Command{
		Use: "guests", Short: "List Proxmox VE guests", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if status != "" && status != "running" && status != "stopped" {
				return fmt.Errorf("--status must be running or stopped")
			}
			_, client, err := openProxmoxClient(endpointName)
			if err != nil {
				return err
			}
			resources, err := client.Resources(context.Background())
			if err != nil {
				return fmt.Errorf("get Proxmox guests: %w", err)
			}
			guests := make([]proxmox.Guest, 0, len(resources.Guests))
			for _, guest := range resources.Guests {
				if (node == "" || guest.Node == node) && (status == "" || guest.Status == status) {
					guests = append(guests, guest)
				}
			}
			return writeProxmox(cmd, guests, jsonOutput, "Guests", func(b *strings.Builder) {
				for _, guest := range guests {
					fmt.Fprintf(b, "%d\t%s\t%s\t%s\t%s\n", guest.VMID, guest.Type, guest.Node, guest.Status, guest.Name)
				}
			})
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name")
	cmd.Flags().StringVar(&node, "node", "", "Filter by node")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (running or stopped)")
	_ = cmd.RegisterFlagCompletionFunc("status", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"running", "stopped"}, cobra.ShellCompDirectiveDefault
	})
	return cmd
}

func newProxmoxNodeCmd() *cobra.Command {
	var endpointName string
	cmd := &cobra.Command{
		Use: "node <name>", Short: "Show Proxmox VE node status", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := openProxmoxClient(endpointName)
			if err != nil {
				return err
			}
			status, err := client.NodeStatus(context.Background(), args[0])
			if err != nil {
				return fmt.Errorf("get Proxmox node %q: %w", args[0], err)
			}
			return writeProxmox(cmd, status, jsonOutput, "Node: "+args[0], func(b *strings.Builder) {
				fmt.Fprintf(b, "PVE version: %s\n", status.PVEVersion)
				if status.CPU != nil {
					fmt.Fprintf(b, "CPU: %.2f%%\n", *status.CPU*100)
				}
				if status.Uptime != nil {
					fmt.Fprintf(b, "Uptime: %ds\n", *status.Uptime)
				}
			})
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name")
	return cmd
}

func newProxmoxTasksCmd() *cobra.Command {
	var endpointName, node string
	var limit int
	cmd := &cobra.Command{
		Use: "tasks", Short: "List recent Proxmox VE tasks", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 {
				return fmt.Errorf("--limit must be positive")
			}
			_, client, err := openProxmoxClient(endpointName)
			if err != nil {
				return err
			}
			nodes := []string{node}
			if node == "" {
				resources, err := client.Resources(context.Background())
				if err != nil {
					return fmt.Errorf("get Proxmox nodes: %w", err)
				}
				nodes = make([]string, len(resources.Nodes))
				for i, resourceNode := range resources.Nodes {
					nodes[i] = resourceNode.Name
				}
			}
			view := proxmoxTasksView{Nodes: nodes, Tasks: make([]proxmox.Task, 0)}
			for _, taskNode := range nodes {
				entries, err := client.TasksLimit(context.Background(), taskNode, limit)
				if err != nil {
					view.Warnings = append(view.Warnings, fmt.Sprintf("tasks for node %q: %v", taskNode, err))
					view.Failed = append(view.Failed, taskNode)
					continue
				}
				view.Tasks = append(view.Tasks, entries...)
			}
			return writeProxmox(cmd, view, jsonOutput, "Tasks", func(b *strings.Builder) {
				if len(view.Nodes) == 0 {
					fmt.Fprintln(b, "No nodes visible; no tasks queried.")
				}
				for _, task := range view.Tasks {
					fmt.Fprintf(b, "%s\t%s\t%s\t%s\n", task.Node, task.Type, task.Status, task.ID)
				}
				for _, warning := range view.Warnings {
					fmt.Fprintf(b, "Warning: %s\n", warning)
				}
			})
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name")
	cmd.Flags().StringVar(&node, "node", "", "Node name")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum tasks per node")
	return cmd
}

func newProxmoxGuestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "guest", Short: "Control a Proxmox VE guest", Args: cobra.NoArgs}
	cmd.AddCommand(
		newProxmoxGuestActionCmd(proxmox.GuestActionStart),
		newProxmoxGuestActionCmd(proxmox.GuestActionShutdown),
		newProxmoxGuestActionCmd(proxmox.GuestActionReboot),
	)
	return cmd
}

func newProxmoxGuestActionCmd(action proxmox.GuestAction) *cobra.Command {
	var endpointName, node, guestType string
	var vmid int
	var confirm bool
	cmd := &cobra.Command{
		Use:   string(action),
		Short: fmt.Sprintf("Submit a Proxmox guest %s action", action),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if endpointName == "" {
				return fmt.Errorf("--endpoint is required for Proxmox guest actions")
			}
			if err := proxmox.ValidateGuestAction(node, guestType, vmid, action); err != nil {
				return err
			}
			if !confirm {
				return fmt.Errorf("confirmation required for Proxmox guest action: endpoint=%q node=%q type=%q vmid=%d action=%q; rerun with --endpoint %q --node %q --type %q --vmid %d --confirm",
					endpointName, node, guestType, vmid, action, endpointName, node, guestType, vmid)
			}
			endpoint, client, err := openProxmoxClient(endpointName)
			if err != nil {
				return err
			}
			upid, err := client.ActOnGuest(context.Background(), node, guestType, vmid, action)
			if err != nil {
				return fmt.Errorf("submit Proxmox guest %s for endpoint %q node %q %s VMID %d: %w", action, endpoint.Name, node, guestType, vmid, err)
			}
			result := proxmoxGuestActionResult{
				Endpoint: endpoint.Name, Node: node, Type: guestType, VMID: vmid,
				Action: action, Status: "accepted", UPID: upid,
			}
			return writeProxmox(cmd, result, jsonOutput, "Guest action accepted", func(b *strings.Builder) {
				fmt.Fprintf(b, "Endpoint: %s\nNode: %s\nType: %s\nVMID: %d\nAction: %s\nUPID: %s\n",
					result.Endpoint, result.Node, result.Type, result.VMID, result.Action, result.UPID)
			})
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name (required)")
	cmd.Flags().StringVar(&node, "node", "", "Proxmox node name (required)")
	cmd.Flags().StringVar(&guestType, "type", "", "Guest type: qemu or lxc (required)")
	cmd.Flags().IntVar(&vmid, "vmid", 0, "Guest VMID (required)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm the explicit guest action target")
	return cmd
}

func newProxmoxTaskCmd() *cobra.Command {
	var endpointName, node string
	cmd := &cobra.Command{
		Use:   "task <upid>",
		Short: "Inspect one Proxmox VE task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if endpointName == "" {
				return fmt.Errorf("--endpoint is required for Proxmox task status")
			}
			if err := proxmox.ValidateTaskStatusRequest(node, args[0]); err != nil {
				return err
			}
			endpoint, client, err := openProxmoxClient(endpointName)
			if err != nil {
				return err
			}
			status, err := client.TaskStatus(context.Background(), node, args[0])
			if err != nil {
				return fmt.Errorf("get Proxmox task %q from endpoint %q node %q: %w", args[0], endpoint.Name, node, err)
			}
			result := proxmoxTaskStatusResult{
				Endpoint: endpoint.Name, Node: node, UPID: args[0], Status: status.Status,
				ExitStatus: status.ExitStatus, Result: status.Result,
			}
			return writeProxmox(cmd, result, jsonOutput, "Task status", func(b *strings.Builder) {
				fmt.Fprintf(b, "Endpoint: %s\nNode: %s\nUPID: %s\nStatus: %s\nResult: %s\n",
					result.Endpoint, result.Node, result.UPID, result.Status, result.Result)
				if result.ExitStatus != "" {
					fmt.Fprintf(b, "Exit status: %s\n", result.ExitStatus)
				}
			})
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name (required)")
	cmd.Flags().StringVar(&node, "node", "", "Proxmox node name (required)")
	return cmd
}

func newProxmoxScriptCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "script", Short: "Show Proxmox VE Community Script install commands", Args: cobra.NoArgs}
	cmd.AddCommand(newProxmoxScriptListCmd(), newProxmoxScriptShowCmd())
	return cmd
}

func newProxmoxScriptListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the curated Proxmox VE Community Scripts catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scripts := proxmox.Scripts()
			return writeProxmox(cmd, scripts, jsonOutput, "Community Scripts", func(b *strings.Builder) {
				for _, script := range scripts {
					fmt.Fprintf(b, "%s\t%s\t%s\n", script.Slug, script.Name, script.Description)
				}
			})
		},
	}
}

func newProxmoxScriptShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <slug>",
		Short: "Print the install command for one Community Script (does not run it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command, err := proxmox.ScriptCommand(args[0])
			if err != nil {
				return err
			}
			result := proxmoxScriptCommandResult{Slug: args[0], Command: command}
			return writeProxmox(cmd, result, jsonOutput, "Community Script command", func(b *strings.Builder) {
				fmt.Fprintf(b, "Slug: %s\nCommand: %s\n\nReview it, then run it yourself on the Proxmox host; homebutler does not run it for you.\n", result.Slug, result.Command)
			})
		},
	}
	return cmd
}

type proxmoxScriptCommandResult struct {
	Slug    string `json:"slug"`
	Command string `json:"command"`
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

type proxmoxTaskStatusResult struct {
	Endpoint   string `json:"endpoint"`
	Node       string `json:"node"`
	UPID       string `json:"upid"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus,omitempty"`
	Result     string `json:"result"`
}

type proxmoxTasksView struct {
	Nodes    []string       `json:"nodes"`
	Tasks    []proxmox.Task `json:"tasks"`
	Warnings []string       `json:"warnings,omitempty"`
	Failed   []string       `json:"failed_collectors,omitempty"`
}

func writeProxmox(cmd *cobra.Command, value any, jsonOutput bool, heading string, human func(*strings.Builder)) error {
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
	}
	var b strings.Builder
	fmt.Fprintln(&b, heading)
	human(&b)
	_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}

func writeProxmoxStatus(cmd *cobra.Command, endpoint string, view proxmox.DefaultView, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(view)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Proxmox endpoint: %s\n", endpoint)
	if view.CollectorFailed(proxmox.CollectorVersion) {
		fmt.Fprintln(&b, "Version: unavailable")
	} else {
		fmt.Fprintf(&b, "Version: %s\n", view.Version.Version)
	}
	if view.CollectorFailed(proxmox.CollectorCluster) {
		fmt.Fprintln(&b, "Cluster: unavailable")
	} else {
		clusterName, quorum := "standalone", "n/a"
		var online, nodes int
		for _, entry := range view.Cluster {
			switch entry.Type {
			case "cluster":
				clusterName = entry.Name
				if entry.Quorate != nil {
					quorum = "no"
					if *entry.Quorate {
						quorum = "yes"
					}
				}
			case "node":
				nodes++
				if entry.Online {
					online++
				}
			}
		}
		fmt.Fprintf(&b, "Cluster: %s | quorum: %s | nodes: %d/%d online\n", clusterName, quorum, online, nodes)
	}
	if view.CollectorFailed(proxmox.CollectorResources) {
		fmt.Fprintln(&b, "Resources: unavailable")
	} else {
		fmt.Fprintf(&b, "Resources: %d nodes | %d guests | %d storage\n", len(view.Resources.Nodes), len(view.Resources.Guests), len(view.Resources.Storage))
	}
	for _, warning := range view.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", warning)
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}
