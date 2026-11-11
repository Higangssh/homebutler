package cmd

import (
	"bufio"
	"errors"
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
		Long:  "Add a remote server's SSH host key to known_hosts. Use --reset when the key has changed: the entries known_hosts already has for the server are replaced once you accept the new fingerprint, and left alone if you do not.",
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

			// The old entries are replaced only after a yes. Removing them
			// first left nothing in known_hosts when the answer was no, and
			// the next key-authenticated connection then trusted whatever
			// answered — in exactly the case --reset exists for.
			fmt.Fprintf(os.Stderr, "connecting to %s (%s:%d)...\n", server.Name, server.Host, server.SSHPort())
			err := remote.TrustServer(server, reset, func(fingerprint string) bool {
				fmt.Fprintf(os.Stderr, "host key fingerprint: %s\n", fingerprint)
				if reset {
					fmt.Fprint(os.Stderr, "replace the host keys known_hosts has for this host with this one? (y/n): ")
				} else {
					fmt.Fprint(os.Stderr, "trust this host? (y/n): ")
				}
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				return strings.TrimSpace(strings.ToLower(answer)) == "y"
			})
			if err != nil {
				// The new key was written; it is the old one that is still
				// accepted, so "left as it was" would be untrue.
				var stale *remote.StillTrustedError
				if errors.As(err, &stale) {
					return err
				}
				if reset {
					return fmt.Errorf("%w; known_hosts was left as it was", err)
				}
				return err
			}

			if reset {
				fmt.Fprintf(os.Stderr, "host key for %s replaced in known_hosts\n", server.Name)
			} else {
				fmt.Fprintf(os.Stderr, "host key for %s added to known_hosts\n", server.Name)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&reset, "reset", false, "Replace the server's existing host keys once the new one is accepted")

	return cmd
}
