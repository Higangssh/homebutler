package cmd

import (
	"github.com/Higangssh/homebutler/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var demo bool

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (JSON-RPC over stdio)",
		Long:  "Start the Model Context Protocol server for AI agent integration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			return mcp.NewServer(cfg, Version, demo).Run()
		},
	}

	cmd.Flags().BoolVar(&demo, "demo", false, "Run with realistic demo data")

	return cmd
}
