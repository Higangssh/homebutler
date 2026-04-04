package alerts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HistoryEntry records a single alert event and its remediation result.
type HistoryEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	Rule         string    `json:"rule"`
	Metric       string    `json:"metric"`
	Details      string    `json:"details"`
	ActionTaken  string    `json:"action_taken"`
	ActionResult string    `json:"action_result"`
}

// defaultHistoryPath returns ~/.homebutler/alerts-history.json.
func defaultHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".homebutler", "alerts-history.json"), nil
}

// RecordHistory appends a history entry to the history file.
func RecordHistory(entry HistoryEntry) error {
	path, err := defaultHistoryPath()
	if err != nil {
		return err
	}
	return RecordHistoryTo(path, entry)
}

// RecordHistoryTo appends a history entry to the given file path.
func RecordHistoryTo(path string, entry HistoryEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create history directory: %w", err)
	}

	entries, _ := LoadHistoryFrom(path) // ignore error for new file
	entries = append(entries, entry)

	// Keep only the last 500 entries
	if len(entries) > 500 {
		entries = entries[len(entries)-500:]
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// LoadHistory reads alert history from the default path.
func LoadHistory() ([]HistoryEntry, error) {
	path, err := defaultHistoryPath()
	if err != nil {
		return nil, err
	}
	return LoadHistoryFrom(path)
}

// LoadHistoryFrom reads alert history from the given file path.
func LoadHistoryFrom(path string) ([]HistoryEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read history: %w", err)
	}

	var entries []HistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse history: %w", err)
	}
	return entries, nil
}

// FormatHistory returns a human-readable table of history entries.
func FormatHistory(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return "No alert history found.\n"
	}

	result := fmt.Sprintf("%-20s %-18s %-10s %-30s %s\n",
		"TIME", "RULE", "ACTION", "DETAILS", "RESULT")

	// Show most recent 20 entries
	start := 0
	if len(entries) > 20 {
		start = len(entries) - 20
	}

	for _, e := range entries[start:] {
		ts := e.Timestamp.Format("2006-01-02 15:04:05")
		details := e.Details
		if len(details) > 30 {
			details = details[:27] + "..."
		}
		result += fmt.Sprintf("%-20s %-18s %-10s %-30s %s\n",
			ts, e.Rule, e.ActionTaken, details, e.ActionResult)
	}
	return result
}
