package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultDrillKeep is how many drill records a drill leaves behind. A drill is
// one small file and the question it answers is "recently enough?", so the
// history is bounded the way watch's incidents and report's snapshots are:
// nothing homebutler writes may grow forever.
const DefaultDrillKeep = 50

// DrillRecord is what a finished drill leaves behind, so the question "has
// this ever been restored" can be answered by something other than a person
// remembering. It is deliberately small — the drill's own output is not worth
// keeping, the verdict and when it happened are.
type DrillRecord struct {
	App     string `json:"app"`
	Archive string `json:"archive"`
	Passed  bool   `json:"passed"`
	// At is when the drill ran, RFC3339 in UTC.
	At string `json:"at"`
}

// DrillsDir is where the records live, beside the backups they are about.
func DrillsDir(backupDir string) string { return filepath.Join(backupDir, ".drills") }

// SaveDrillRecord writes one record and prunes the directory to keep.
func SaveDrillRecord(backupDir string, rec DrillRecord, keep int) error {
	dir := DrillsDir(backupDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create drill record directory: %w", err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.json", strings.ReplaceAll(rec.At, ":", ""), safeName(rec.App))
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return err
	}

	// The record is on disk. A failed prune is housekeeping, not a reason to
	// tell the caller the drill was not recorded — the same call this makes
	// as watch's SaveIncident.
	if err := pruneDrillRecords(dir, keep); err != nil {
		fmt.Fprintf(os.Stderr, "warning: prune drill records: %v\n", err)
	}
	return nil
}

// ListDrillRecords returns every record, newest first.
func ListDrillRecords(backupDir string) ([]DrillRecord, error) {
	entries, err := os.ReadDir(DrillsDir(backupDir))
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing has ever been drilled. That is an answer, not a failure:
			// it is the state every install starts in.
			return nil, nil
		}
		return nil, err
	}

	var out []DrillRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(DrillsDir(backupDir), e.Name()))
		if err != nil {
			continue
		}
		var rec DrillRecord
		if err := json.Unmarshal(data, &rec); err != nil || rec.At == "" {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out, nil
}

// LatestDrill returns the most recent record and whether there is one.
func LatestDrill(records []DrillRecord) (DrillRecord, bool) {
	if len(records) == 0 {
		return DrillRecord{}, false
	}
	return records[0], true
}

// ParseDrillTime reads a record's timestamp.
func ParseDrillTime(rec DrillRecord) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, rec.At)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func pruneDrillRecords(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return nil
	}
	// The name begins with the timestamp, so lexical order is chronological.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names[keep:] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// safeName keeps a record filename from leaving the directory it belongs in:
// the app name reaches here from a command line.
func safeName(app string) string {
	var b strings.Builder
	for _, r := range app {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
