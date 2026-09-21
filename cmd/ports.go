package cmd

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/spf13/cobra"
)

func newPortsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports",
		Args:  cobra.NoArgs,
		Short: "List open ports with process info",
		Long:  "List all open TCP/UDP ports and their associated processes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := ports.List()
			if err != nil {
				return err
			}
			// A port published by Docker is held by root's docker-proxy, so an
			// ordinary user gets no process name for it. The container that
			// published it is known, and report names it — this command has to
			// agree with that one.
			if containers, dockerErr := docker.List(); dockerErr == nil {
				result.Ports = inventory.AttributePorts(result.Ports, containers)
			}
			if err := output(result.Ports, jsonOutput); err != nil {
				return err
			}
			if result.MissingProcess && !jsonOutput {
				fmt.Fprintf(os.Stderr, "\n⚠️  Some process names are missing. Try: sudo homebutler ports\n")
			}
			return nil
		},
	}
}
