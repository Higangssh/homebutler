package cmd

import (
	"time"

	"github.com/Higangssh/homebutler/internal/network"
	"github.com/spf13/cobra"
)

func newNetworkCmd() *cobra.Command {
	networkCmd := &cobra.Command{
		Use:   "network",
		Short: "Network utilities",
		Long:  "Network-related commands for scanning and diagnostics.",
	}

	networkCmd.AddCommand(newNetworkScanCmd())

	return networkCmd
}

func newNetworkScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Discover devices on local network",
		Long:  "Scan the local network to discover connected devices.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			devices, err := network.ScanWithTimeout(30 * time.Second)
			if err != nil {
				return err
			}
			return output(devices, jsonOutput)
		},
	}
}
