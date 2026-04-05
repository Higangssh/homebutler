package cmd

import (
	"github.com/Higangssh/homebutler/internal/server"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var host string
	var port int
	var demo bool
	var token string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Web dashboard (default port 8080)",
		Long:  "Start the homebutler web dashboard. Use --demo for realistic demo data without real system calls.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			srv := server.New(cfg, host, port, demo)
			srv.SetVersion(Version)
			if token != "" {
				srv.SetToken(token)
			}
			return srv.Run()
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host to bind to")
	cmd.Flags().IntVar(&port, "port", 8080, "Port for the web dashboard")
	cmd.Flags().BoolVar(&demo, "demo", false, "Run with realistic demo data (no real system calls)")
	cmd.Flags().StringVar(&token, "token", "", "Bearer token for API authentication")

	return cmd
}
