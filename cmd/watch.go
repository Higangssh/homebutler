package cmd

import (
	"github.com/Higangssh/homebutler/internal/tui"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "TUI dashboard (monitors all configured servers)",
		Long:  "Launch the terminal UI dashboard that monitors all configured servers in real-time.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			return tui.Run(cfg, nil)
		},
	}
}
