package cmd

import (
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/spf13/cobra"
)

func newPortsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports",
		Short: "List open ports with process info",
		Long:  "List all open TCP/UDP ports and their associated processes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			openPorts, err := ports.List()
			if err != nil {
				return err
			}
			return output(openPorts, jsonOutput)
		},
	}
}
