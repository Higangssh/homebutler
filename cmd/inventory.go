package cmd

import (
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/spf13/cobra"
)

func newInventoryCmd() *cobra.Command {
	invCmd := &cobra.Command{
		Use:   "inventory",
		Short: "Collect and display server inventory/topology",
		Long:  "Scan local server to collect system status, Docker containers, and open ports, then display as a tree or export as Mermaid diagram.",
	}

	invCmd.AddCommand(
		newInventoryScanCmd(),
		newInventoryShowCmd(),
		newInventoryExportCmd(),
	)

	return invCmd
}

func newInventoryScanCmd() *cobra.Command {
	var filter string
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan and display current server inventory",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInventoryScan(filter)
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "Filter inventory output (supported: exposed)")
	return cmd
}

func newInventoryShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current server inventory (same as scan)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInventoryScan("")
		},
	}
}

func runInventoryScan(filter string) error {
	if filter != "" && !inventory.IsSupportedFilter(filter) {
		return fmt.Errorf("unsupported filter %q (supported: %s)", filter, strings.Join(inventory.SupportedFilters(), ", "))
	}
	if filter != "" && jsonOutput {
		return fmt.Errorf("--filter is not supported with --json; JSON output is always the full inventory")
	}

	if err := loadConfig(); err != nil {
		return err
	}

	inv, err := inventory.Collect(cfg, inventory.DefaultCollectFuncs())
	if err != nil {
		return fmt.Errorf("inventory scan failed: %w", err)
	}

	if jsonOutput {
		return output(inv, true)
	}

	if filter != "" {
		out, err := inventory.RenderTreeFiltered(inv, filter)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}

	fmt.Print(inventory.RenderTree(inv))
	return nil
}

func newInventoryExportCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export inventory in a structured format",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			inv, err := inventory.Collect(cfg, inventory.DefaultCollectFuncs())
			if err != nil {
				return fmt.Errorf("inventory export failed: %w", err)
			}

			if jsonOutput {
				return output(inv, true)
			}

			switch format {
			case "mermaid":
				fmt.Print(inventory.RenderMermaid(inv))
			default:
				return fmt.Errorf("unsupported format: %q (supported: mermaid)", format)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "mermaid", "Export format (mermaid)")
	return cmd
}
