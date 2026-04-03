package cmd

import (
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/system"
	"github.com/spf13/cobra"
)

func newProcessesCmd() *cobra.Command {
	var sortBy string
	var limit int

	cmd := &cobra.Command{
		Use:     "processes",
		Aliases: []string{"ps"},
		Short:   "Show top processes by resource usage",
		Long:    "Display top processes sorted by CPU or memory usage, with zombie detection.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}
			result, err := system.ListProcesses(limit, sortBy)
			if err != nil {
				return err
			}
			if jsonOutput {
				return output(result, true)
			}

			// Human-readable output
			sortLabel := "CPU"
			if sortBy == "mem" {
				sortLabel = "Memory"
			}
			fmt.Fprintf(os.Stdout, "\n📊 Top processes (by %s)\n\n", sortLabel)
			fmt.Fprintf(os.Stdout, "  %6s  %5s  %5s  %s\n", "PID", "CPU%", "MEM%", "PROCESS")
			for _, p := range result.Processes {
				fmt.Fprintf(os.Stdout, "  %6d  %5.1f  %5.1f  %s\n", p.PID, p.CPU, p.Mem, p.Name)
			}

			// Summary line
			zombieCount := len(result.Zombies)
			if zombieCount > 0 {
				fmt.Fprintf(os.Stdout, "\nTotal: %d processes | 🧟 %d zombies ⚠️\n", result.Total, zombieCount)
				for _, z := range result.Zombies {
					fmt.Fprintf(os.Stdout, "  PID %d: [defunct] %s\n", z.PID, z.Name)
				}
			} else {
				fmt.Fprintf(os.Stdout, "\nTotal: %d processes | 🧟 0 zombies\n", result.Total)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sortBy, "sort", "cpu", "Sort by: cpu, mem")
	cmd.Flags().IntVar(&limit, "limit", 10, "Number of processes to show (0 = all)")

	return cmd
}
