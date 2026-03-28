package cmd

import (
	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	var service string
	var backupTo string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup all Docker service volumes",
		Long: `Backup Docker service volumes to a tar archive.

Use --service to backup a specific service only.
Use --to to specify a custom backup destination.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}

			backupDir := backupTo
			if backupDir == "" {
				backupDir = cfg.ResolveBackupDir()
			}

			result, err := backup.Run(backupDir, service)
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "Backup a specific service only")
	cmd.Flags().StringVar(&backupTo, "to", "", "Custom backup destination")

	cmd.AddCommand(newBackupListCmd())

	return cmd
}

func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List existing backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}

			backupDir := cfg.ResolveBackupDir()
			entries, err := backup.List(backupDir)
			if err != nil {
				return err
			}
			return output(entries, jsonOutput)
		},
	}
}

func newRestoreCmd() *cobra.Command {
	var service string

	cmd := &cobra.Command{
		Use:   "restore <archive>",
		Short: "Restore volumes from a backup archive",
		Long:  "Restore Docker service volumes from a previously created backup archive.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}
			if handled, err := maybeRouteRemote(); handled {
				return err
			}

			result, err := backup.Restore(args[0], service)
			if err != nil {
				return err
			}
			return output(result, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "Restore a specific service only")

	return cmd
}
