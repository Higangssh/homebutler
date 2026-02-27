package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

func runUpgrade(cfg *config.Config, currentVersion string) error {
	localOnly := hasFlag("--local")
	jsonOutput := hasFlag("--json")

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
	localResult := remote.SelfUpgrade(currentVersion, latestVersion)
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
