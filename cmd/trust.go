package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/spf13/cobra"
)

func newTrustCmd() *cobra.Command {
	var reset bool

	cmd := &cobra.Command{
		Use:   "trust <server>",
		Short: "Trust a remote server's SSH host key",
		Long:  "Add a remote server's SSH host key to known_hosts. Use --reset to remove old keys first.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			serverArg := args[0]
			server := cfg.FindServer(serverArg)
			if server == nil {
				return fmt.Errorf("server %q not found in config. Available servers: %s", serverArg, listServerNames(cfg))
			}

			if reset {
				fmt.Fprintf(os.Stderr, "removing old host keys for %s...\n", server.Name)
				if err := remote.RemoveHostKeys(server); err != nil {
					return fmt.Errorf("failed to remove old keys: %w", err)
				}
			}

			fmt.Fprintf(os.Stderr, "connecting to %s (%s:%d)...\n", server.Name, server.Host, server.SSHPort())
			err := remote.TrustServer(server, func(fingerprint string) bool {
				fmt.Fprintf(os.Stderr, "host key fingerprint: %s\n", fingerprint)
				fmt.Fprint(os.Stderr, "trust this host? (y/n): ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				return strings.TrimSpace(strings.ToLower(answer)) == "y"
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "host key for %s added to known_hosts\n", server.Name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&reset, "reset", false, "Remove old host key before re-trusting")

	return cmd
}
