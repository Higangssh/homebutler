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
			srv := mcp.NewServer(cfg, Version, demo)
			// So config_validate checks the file this server is running on
			// rather than resolving one of its own.
			srv.SetConfigPath(cfgPath)
			return srv.Run()
		},
	}

	cmd.Flags().BoolVar(&demo, "demo", false, "Run with realistic demo data")

	return cmd
}
