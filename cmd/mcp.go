package cmd

import (
	"github.com/Higangssh/homebutler/internal/capability"
	"github.com/Higangssh/homebutler/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var demo bool
	var maxRisk string

	cmd := &cobra.Command{
		Use:   "mcp",
		Args:  cobra.NoArgs,
		Short: "Start MCP server (JSON-RPC over stdio)",
		Long:  "Start the Model Context Protocol server for AI agent integration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			srv := mcp.NewServer(cfg, Version, demo)
			if err := srv.SetMaxRisk(capability.Risk(maxRisk)); err != nil {
				return err
			}
			// So config_validate checks the file this server is running on
			// rather than resolving one of its own.
			srv.SetConfigPath(cfgPath)
			return srv.Run()
		},
	}

	cmd.Flags().BoolVar(&demo, "demo", false, "Run with realistic demo data")
	cmd.Flags().StringVar(&maxRisk, "max-risk", string(capability.RiskDestructive),
		"Highest tool risk to offer: read, write or destructive. With read, an agent can look but this server changes nothing; report runs without saving a snapshot")

	return cmd
}
