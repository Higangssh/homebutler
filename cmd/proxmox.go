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
	cmd.AddCommand(newProxmoxStatusCmd(), newProxmoxGuestsCmd(), newProxmoxNodeCmd(), newProxmoxTasksCmd())
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
	endpoint, err := selectProxmoxEndpoint(cfg, endpointName)
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
			tasks := make([]proxmox.Task, 0)
			for _, taskNode := range nodes {
				entries, err := client.TasksLimit(context.Background(), taskNode, limit)
				if err != nil {
					return fmt.Errorf("get Proxmox tasks for node %q: %w", taskNode, err)
				}
				tasks = append(tasks, entries...)
			}
			return writeProxmox(cmd, tasks, jsonOutput, "Tasks", func(b *strings.Builder) {
				for _, task := range tasks {
					fmt.Fprintf(b, "%s\t%s\t%s\t%s\n", task.Node, task.Type, task.Status, task.ID)
				}
			})
		},
	}
	cmd.Flags().StringVar(&endpointName, "endpoint", "", "Proxmox endpoint name")
	cmd.Flags().StringVar(&node, "node", "", "Node name")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum tasks per node")
	return cmd
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

func selectProxmoxEndpoint(c *config.Config, name string) (*config.ProxmoxConfig, error) {
	if name != "" {
		endpoint := c.FindProxmox(name)
		if endpoint == nil {
			return nil, fmt.Errorf("proxmox endpoint %q not found in config. Available endpoints: %s", name, listProxmoxNames(c))
		}
		return endpoint, nil
	}
	if len(c.Proxmox) == 0 {
		return nil, fmt.Errorf("no Proxmox endpoints configured. Add proxmox entries to your config file")
	}
	if len(c.Proxmox) > 1 {
		return nil, fmt.Errorf("multiple proxmox endpoints configured; use --endpoint. Available endpoints: %s", listProxmoxNames(c))
	}
	return &c.Proxmox[0], nil
}

func listProxmoxNames(c *config.Config) string {
	names := make([]string, len(c.Proxmox))
	for i, endpoint := range c.Proxmox {
		names[i] = endpoint.Name
	}
	return fmt.Sprintf("%v", names)
}

func writeProxmoxStatus(cmd *cobra.Command, endpoint string, view proxmox.DefaultView, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(view)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Proxmox endpoint: %s\n", endpoint)
	fmt.Fprintf(&b, "Version: %s\n", view.Version.Version)
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
	fmt.Fprintf(&b, "Resources: %d nodes | %d guests | %d storage\n", len(view.Resources.Nodes), len(view.Resources.Guests), len(view.Resources.Storage))
	_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}
