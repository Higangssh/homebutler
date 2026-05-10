package cmd

import (
	"fmt"
	"time"

	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var strict bool
	var backupMaxAge time.Duration

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose homelab health, exposure, backups, and readiness",
		Long: `Run a read-only diagnosis for the things that usually hurt self-hosted servers:
resource pressure, stopped containers, public bind ports, backup hygiene,
notification readiness, and report baseline status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}

			result, err := doctor.Run(cfg, doctor.DefaultCollectFuncs(), doctor.Options{
				BackupMaxAge: backupMaxAge,
				Strict:       strict,
			})
			if err != nil {
				return fmt.Errorf("doctor failed: %w", err)
			}

			if jsonOutput {
				if err := output(result, true); err != nil {
					return err
				}
			} else {
				fmt.Print(doctor.FormatHuman(result))
			}

			if strict && result.Status != doctor.SeverityPass {
				return fmt.Errorf("doctor found %d warning(s) and %d failure(s)", result.Summary.Warn, result.Summary.Fail)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero when warnings or failures are found")
	cmd.Flags().DurationVar(&backupMaxAge, "backup-max-age", 7*24*time.Hour, "Warn when the latest backup is older than this duration")

	return cmd
}
