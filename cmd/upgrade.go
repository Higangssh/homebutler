package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/util"
	"github.com/spf13/cobra"
)

// The upgrades themselves, swapped out by tests.
var (
	selfUpgrade   = remote.SelfUpgrade
	remoteUpgrade = remote.RemoteUpgrade
)

func newUpgradeCmd() *cobra.Command {
	var localOnly bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Args:  cobra.NoArgs,
		Short: "Upgrade local + all remote servers to latest",
		Long:  "Upgrade homebutler on the local machine and all configured remote servers to the latest version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			// Fetch latest version
			fmt.Fprintf(os.Stderr, "checking latest version... ")
			latestVersion, err := fetchLatestVersion()
			if err != nil {
				return fmt.Errorf("cannot check latest version: %w", err)
			}
			fmt.Fprintf(os.Stderr, "v%s\n\n", latestVersion)

			var report remote.UpgradeReport
			report.LatestVersion = latestVersion

			// 1. Self-upgrade
			fmt.Fprintf(os.Stderr, "upgrading local... ")
			localResult := selfUpgrade(Version, latestVersion)
			report.Results = append(report.Results, *localResult)
			printUpgradeStatus(localResult)

			// 2. Remote servers (unless --local)
			if !localOnly {
				for _, srv := range cfg.Servers {
					if srv.Local {
						continue
					}
					fmt.Fprintf(os.Stderr, "upgrading %s... ", srv.Name)
					result := remoteUpgrade(&srv, latestVersion)
					report.Results = append(report.Results, *result)
					printUpgradeStatus(result)
				}
			}

			// Summary
			upgraded, upToDate, skipped, failed := 0, 0, 0, 0
			for _, r := range report.Results {
				switch r.Status {
				case "upgraded":
					upgraded++
				case "up-to-date":
					upToDate++
				case "skipped":
					skipped++
				case "error":
					failed++
				}
			}
			fmt.Fprintf(os.Stderr, "\n")
			parts := []string{}
			if upgraded > 0 {
				parts = append(parts, fmt.Sprintf("%d upgraded", upgraded))
			}
			if upToDate > 0 {
				parts = append(parts, fmt.Sprintf("%d already up-to-date", upToDate))
			}
			if skipped > 0 {
				parts = append(parts, fmt.Sprintf("%d skipped", skipped))
			}
			if failed > 0 {
				parts = append(parts, fmt.Sprintf("%d failed", failed))
			}
			for i, p := range parts {
				if i > 0 {
					fmt.Fprint(os.Stderr, ", ")
				}
				fmt.Fprint(os.Stderr, p)
			}
			fmt.Fprint(os.Stderr, "\n")

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
			}

			return upgradeVerdict(report.Results)
		},
	}

	cmd.Flags().BoolVar(&localOnly, "local", false, "Upgrade only the local binary (skip remote servers)")

	return cmd
}

func printUpgradeStatus(r *remote.UpgradeResult) {
	switch r.Status {
	case "upgraded":
		fmt.Fprintf(os.Stderr, "✓ %s\n", r.Message)
	case "up-to-date", "skipped":
		fmt.Fprintf(os.Stderr, "─ %s\n", r.Message)
	case "error":
		message := r.Message
		if r.Error != nil {
			message = util.FormatError(r.Error, verboseOutput || jsonOutput)
		}
		fmt.Fprintf(os.Stderr, "✗ %s\n", message)
	}
}

// upgradeVerdict fails the command when any target, the local one included,
// could not be upgraded. Already up to date is not a failure, and nor is a
// dev build left alone.
func upgradeVerdict(results []remote.UpgradeResult) error {
	var failed []string
	for _, r := range results {
		if r.Status == "error" {
			failed = append(failed, r.Target)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	noun := "targets"
	if len(results) == 1 {
		noun = "target"
	}
	return fmt.Errorf("upgrade failed on %d of %d %s: %s", len(failed), len(results), noun, strings.Join(failed, ", "))
}
