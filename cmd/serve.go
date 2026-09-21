package cmd

import (
	"fmt"

	"github.com/Higangssh/homebutler/internal/server"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var host string
	var port int
	var demo bool
	var token string

	cmd := &cobra.Command{
		Use:   "serve",
		Args:  cobra.NoArgs,
		Short: "Web dashboard (default port 8080)",
		Long:  "Start the homebutler web dashboard. Use --demo for realistic demo data without real system calls.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			// The image exists to be reached from another machine, which is
			// exactly when an unauthenticated dashboard is wrong. Outside a
			// container this stays a warning on the settings screen: somebody
			// running it on a LAN behind their own reverse proxy has made a
			// choice, and a container published to a network has not.
			if token == "" && system.InContainer() && host != "127.0.0.1" && host != "localhost" && host != "::1" {
				return fmt.Errorf("refusing to serve %s from a container without --token: this dashboard would be reachable from the network with nothing in front of it\n  → pass --token, or bind 127.0.0.1 and put a proxy in front", host)
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
