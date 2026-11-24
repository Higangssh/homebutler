package cmd

import (
	"fmt"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/spf13/cobra"
)

// runDoctor is doctor.Run, swapped by tests to see the options the command
// builds without running a diagnosis of the machine running the tests.
var runDoctor = doctor.Run

func newDoctorCmd() *cobra.Command {
	var strict bool
	var noSave bool
	var backupMaxAge time.Duration

	cmd := &cobra.Command{
		Use:   "doctor",
		Args:  cobra.NoArgs,
		Short: "Diagnose homelab health, exposure, backups, and readiness",
		Long: `Run a read-only diagnosis for the things that usually hurt self-hosted servers:
resource pressure, stopped containers, public bind ports, backup hygiene,
notification readiness including whether watch will actually send anything, report baseline status, whether anything is installed
to watch what you asked it to watch, config file permissions, containers that
mount the Docker socket, Proxmox endpoints that accept any certificate, incident
history nearing its limit, configured Proxmox endpoint reachability, TLS
certificates that have expired, expire within 14 days, or do not name their
host (every entry under certificates: in the config, plus each Proxmox
endpoint), and pending security updates and a pending reboot as apt or dnf
already knows them — read without root or the network, and said to be stale
when the package list is more than a week old.

It also asks each running container's registry, anonymously, which image its
tag names now, and says when that is a different image than the one running.
Answers are kept in ~/.homebutler/registry/digests.json for
doctor.image_updates.cache_ttl (6h). Nothing is pulled or restarted, and these
findings are info, which never changes the exit code. Set
doctor.image_updates.enabled to false to stop it. Under --server, the
server's own config decides, because the check runs there.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				// doctor is the command that explains what is wrong, so a
				// refusal it can name has to reach the checks rather than the
				// exit code. LoadForDiagnosis differs from Load only in the
				// permission refusal, so succeeding here means that was it.
				permissive, perr := config.LoadForDiagnosis(config.Resolve(cfgPath))
				if perr != nil {
					return err
				}
				cfg = permissive
			}
			if handled, err := maybeRouteRemote(cmd); handled {
				return err
			}

			result, err := runDoctor(cfg, doctor.DefaultCollectFuncs(), doctor.Options{
				BackupMaxAge: backupMaxAge,
				Strict:       strict,
				NoSave:       noSave,
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
	cmd.Flags().BoolVar(&noSave, "no-save", false, "Read the cache of registry answers without writing it, so doctor writes nothing")
	cmd.Flags().DurationVar(&backupMaxAge, "backup-max-age", 7*24*time.Hour, "Warn when the latest backup is older than this duration")

	return cmd
}
