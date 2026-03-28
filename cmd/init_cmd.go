package cmd

import (
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard (creates config)",
		Long:  "Run the interactive setup wizard to create or update the homebutler configuration file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}
}
