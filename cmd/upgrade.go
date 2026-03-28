package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/spf13/cobra"
)

func newUpgradeCmd() *cobra.Command {
	var localOnly bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade local + all remote servers to latest",
		Long:  "Upgrade homebutler on the local machine and all configured remote servers to the latest version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			// Fetch latest version
			fmt.Fprintf(os.Stderr, "checking latest version... ")
			latestVersion, err := remote.FetchLatestVersion()
			if err != nil {
				return fmt.Errorf("cannot check latest version: %w", err)
			}
			fmt.Fprintf(os.Stderr, "v%s\n\n", latestVersion)

			var report remote.UpgradeReport
			report.LatestVersion = latestVersion

			// 1. Self-upgrade
			fmt.Fprintf(os.Stderr, "upgrading local... ")
			localResult := remote.SelfUpgrade(Version, latestVersion)
			report.Results = append(report.Results, *localResult)
			printUpgradeStatus(localResult)

			// 2. Remote servers (unless --local)
			if !localOnly {
				for _, srv := range cfg.Servers {
					if srv.Local {
						continue
					}
					fmt.Fprintf(os.Stderr, "upgrading %s... ", srv.Name)
					result := remote.RemoteUpgrade(&srv, latestVersion)
					report.Results = append(report.Results, *result)
					printUpgradeStatus(result)
				}
			}

			// Summary
			upgraded, upToDate, failed := 0, 0, 0
			for _, r := range report.Results {
				switch r.Status {
				case "upgraded":
					upgraded++
				case "up-to-date":
					upToDate++
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
				return enc.Encode(report)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&localOnly, "local", false, "Upgrade only the local binary (skip remote servers)")

	return cmd
}

func printUpgradeStatus(r *remote.UpgradeResult) {
	switch r.Status {
	case "upgraded":
		fmt.Fprintf(os.Stderr, "✓ %s\n", r.Message)
	case "up-to-date":
		fmt.Fprintf(os.Stderr, "─ %s\n", r.Message)
	case "error":
		fmt.Fprintf(os.Stderr, "✗ %s\n", r.Message)
	}
}
