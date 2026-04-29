package cmd

import (
	"fmt"

	"github.com/Higangssh/homebutler/internal/report"
	"github.com/spf13/cobra"
)

func newReportCmd() *cobra.Command {
	var keep int
	var noSave bool

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a concise butler-style status report",
		Long: `Collect current system, container, and port state into a snapshot,
compare against the previous snapshot, and print a human-readable report.

Snapshots are stored in ~/.homebutler/reports/snapshots/ and pruned to
the most recent --keep entries (default 30).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}

			r, err := report.Run(cfg, report.DefaultCollectFuncs(), report.Options{
				Keep:   keep,
				NoSave: noSave,
			})
			if err != nil {
				return fmt.Errorf("report failed: %w", err)
			}

			if jsonOutput {
				return output(r, true)
			}
			fmt.Print(report.FormatHuman(r))
			return nil
		},
	}

	cmd.Flags().IntVar(&keep, "keep", 30, "Number of daily snapshots to retain (minimum 1)")
	cmd.Flags().BoolVar(&noSave, "no-save", false, "Print report without writing a snapshot")

	return cmd
}
