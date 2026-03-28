package cmd

import (
	"github.com/Higangssh/homebutler/internal/wake"
	"github.com/spf13/cobra"
)

func newWakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wake <mac-address|name> [broadcast]",
		Short: "Send Wake-on-LAN magic packet",
		Long:  "Send a Wake-on-LAN magic packet to a device by MAC address or configured name.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}

			target := args[0]
			broadcast := "255.255.255.255"

			// Check if target is a name from config
			if wt := cfg.FindWakeTarget(target); wt != nil {
				target = wt.MAC
				if wt.Broadcast != "" {
					broadcast = wt.Broadcast
				}
			}

			// Use second positional arg as broadcast if provided
			if len(args) >= 2 {
				broadcast = args[1]
			}

			result, err := wake.Send(target, broadcast)
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}
}
