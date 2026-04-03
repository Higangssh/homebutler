package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/spf13/cobra"
)

func newPortsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports",
		Short: "List open ports with process info",
		Long:  "List all open TCP/UDP ports and their associated processes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := ports.List()
			if err != nil {
				return err
			}
			if err := output(result.Ports, jsonOutput); err != nil {
				return err
			}
			if result.MissingProcess && !jsonOutput {
				hint := "sudo homebutler ports"
				if runtime.GOOS == "darwin" {
					hint = "sudo homebutler ports"
				}
				fmt.Fprintf(os.Stderr, "\n⚠️  Some process names are missing. Try: %s\n", hint)
			}
			return nil
		},
	}
}
