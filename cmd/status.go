package cmd

import (
	"fmt"

	"github.com/Higangssh/homebutler/internal/system"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "System status (CPU, memory, disk, uptime)",
		Long:  "Display current system status including CPU usage, memory, disk space, and uptime.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			info, err := system.Status()
			if err != nil {
				return fmt.Errorf("failed to get system status: %w", err)
			}
			return output(info, jsonOutput)
		},
	}
}
