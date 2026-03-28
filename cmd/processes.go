package cmd

import (
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/spf13/cobra"
)

func newProcessesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "processes",
		Short: "Show top processes by resource usage",
		Long:  "Display the top 10 processes sorted by resource consumption.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			procs, err := system.TopProcesses(10)
			if err != nil {
				return err
			}
			return output(procs, jsonOutput)
		},
	}
}
