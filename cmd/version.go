package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Aliases: []string{},
		Short:   "Print version",
		Long:    "Print the homebutler version, build date, and Go runtime version.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("homebutler %s (built %s, %s)\n", Version, BuildDate, runtime.Version())
		},
	}
}
