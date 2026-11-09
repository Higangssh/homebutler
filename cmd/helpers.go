package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/format"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/util"
	"github.com/Higangssh/homebutler/internal/wake"
)

// Global state set from main.go
var (
	Version   = "dev"
	BuildDate = "unknown"
)

// Global flags
var (
	jsonOutput    bool
	verboseOutput bool
	serverName    string
	allServers    bool
	cfgPath       string
)

// cfg holds the loaded config (set in root PersistentPreRun)
var cfg *config.Config

// writeJSON is what --json prints, to any writer.
func writeJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(util.EmptyLists(data))
}

// output renders data as JSON or human-readable format.
func output(data any, jsonOut bool) error {
	if jsonOut {
		return writeJSON(os.Stdout, data)
	}

	switch v := data.(type) {
	case *system.StatusInfo:
		fmt.Print(format.Status(v))
	case []docker.Container:
		fmt.Print(format.DockerList(v))
	case *docker.ActionResult:
		fmt.Print(format.DockerAction(v.Action, v.Container))
	case []docker.ContainerStats:
		fmt.Print(format.DockerStats(v))
	case *docker.LogsResult:
		fmt.Printf("=== %s (last %s lines) ===\n%s\n", v.Container, v.Lines, v.Logs)
	case *docker.TopResult:
		fmt.Print(format.DockerTop(v))
	case *docker.InspectResult:
		fmt.Print(format.DockerInspect(v))
	case *alerts.AlertResult:
		fmt.Print(format.Alerts(v))
	case []ports.PortInfo:
		fmt.Print(format.Ports(v))
	case []network.Device:
		fmt.Print(format.NetworkScan(v))
	case *wake.WakeResult:
		fmt.Print(format.WakeResult(v.MAC, v.Broadcast))
	case *backup.BackupResult:
		fmt.Printf("Backup complete: %s\n", v.Archive)
		fmt.Printf("  Services: %s\n", strings.Join(v.Services, ", "))
		fmt.Printf("  Volumes:  %d\n", v.Volumes)
		fmt.Printf("  Size:     %s\n", v.Size)
		if p := v.Pruned; p != nil && len(p.Removed) > 0 {
			// Named individually. Deleting a backup can be deleting the last
			// copy of something, so which ones went is not a detail.
			fmt.Printf("\n  Retention removed %d older backup(s), freeing %s:\n", len(p.Removed), backup.FormatSize(p.Freed))
			for _, name := range p.Removed {
				fmt.Printf("    %s\n", name)
			}
			fmt.Printf("  %d kept, %s total.\n", p.Kept, backup.FormatSize(p.KeptSum))
		}
		// Named individually, for the same reason the removed backups are.
		// What an archive does not contain is the thing somebody restoring
		// from it most needs to have been told, and the archive is where they
		// will read it — this is only the copy at the time.
		if len(v.Skipped) > 0 {
			fmt.Printf("\n  Excluded %d bind mount(s), recorded in the archive:\n", len(v.Skipped))
			for _, name := range v.Skipped {
				fmt.Printf("    %s\n", name)
			}
		}
		// A misspelled path backs up the terabytes it was meant to avoid, and
		// the archive size is the only other place that shows up.
		if len(v.UnmatchedExcludes) > 0 {
			fmt.Printf("\n⚠️  %d --exclude path(s) matched no bind mount, so nothing was left out for them:\n", len(v.UnmatchedExcludes))
			for _, name := range v.UnmatchedExcludes {
				fmt.Printf("    %s\n", name)
			}
		}
	case *backup.RestoreResult:
		fmt.Printf("Restore complete from: %s\n", v.Archive)
		fmt.Printf("  Services: %s\n", strings.Join(v.Services, ", "))
		fmt.Printf("  Volumes:  %d\n", v.Volumes)
		if len(v.Refused) > 0 {
			fmt.Printf("\n⚠️  Refused %d mount(s) the archive asked for:\n", len(v.Refused))
			for _, r := range v.Refused {
				fmt.Printf("  %s (%s, service %s)\n    → %s\n", r.Target, r.Type, r.Service, r.Reason)
			}
		}
	case *backup.DrillResult:
		fmt.Print(v.String())
	case *backup.DrillReport:
		fmt.Print(v.String())
	case []backup.ListEntry:
		if len(v) == 0 {
			fmt.Println("No backups found.")
		} else {
			fmt.Printf("%-40s %-10s %s\n", "NAME", "SIZE", "CREATED")
			for _, e := range v {
				fmt.Printf("%-40s %-10s %s\n", e.Name, e.Size, e.CreatedAt)
			}
		}
	default:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(util.EmptyLists(data))
	}
	return nil
}

func formatCommandError(err error) error {
	if err == nil || jsonOutput {
		return err
	}
	return errors.New(util.FormatError(err, verboseOutput))
}

// listServerNames returns a formatted list of server names from config.
func listServerNames(c *config.Config) string {
	if len(c.Servers) == 0 {
		return "(none configured)"
	}
	names := make([]string, len(c.Servers))
	for i, s := range c.Servers {
		names[i] = s.Name
	}
	return fmt.Sprintf("%v", names)
}

// filterFlags removes specified flags (and their values) from args.
func filterFlags(args []string, flags ...string) []string {
	skip := make(map[string]bool)
	for _, f := range flags {
		skip[f] = true
	}
	var filtered []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if skip[arg] {
			if valueFlags[arg] {
				skipNext = true
			}
			continue
		}
		// --all=true and --server=rpi5 are the same flags written another
		// way; forwarded, the remote fans out again or looks for itself.
		if name, _, ok := strings.Cut(arg, "="); ok && skip[name] {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// valueFlags are flags that take a value argument.
var valueFlags = map[string]bool{
	"--server":         true,
	"--config":         true,
	"--local":          true,
	"--port":           true,
	"--service":        true,
	"--to":             true,
	"--backup-max-age": true,
	"--endpoint":       true,
}

// isFlag checks if a string looks like a CLI flag.
func isFlag(s string) bool {
	return len(s) > 1 && s[0] == '-'
}
