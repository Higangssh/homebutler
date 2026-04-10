package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PM2Monitor watches PM2 processes by polling `pm2 jlist`.
type PM2Monitor struct {
	// Run executes an external command. Required.
	Run CommandRunner

	// Dir is the storage directory for incidents.
	Dir string

	// Interval is the polling interval.
	Interval time.Duration

	// ReadFile reads a file's contents. Nil defaults to os.ReadFile.
	ReadFile func(path string) ([]byte, error)
}

type pm2Process struct {
	Name   string `json:"name"`
	PM2Env pm2Env `json:"pm2_env"`
}

type pm2Env struct {
	RestartTime int    `json:"restart_time"`
	Status      string `json:"status"`
}

func (pm *PM2Monitor) parseProcesses(output string) []pm2Process {
	var procs []pm2Process
	if err := json.Unmarshal([]byte(output), &procs); err != nil {
		return nil
	}
	return procs
}

// Watch polls PM2 process list and sends incidents when restart_time changes.
func (pm *PM2Monitor) Watch(ctx context.Context, targets []Target, incidents chan<- Incident) error {
	if len(targets) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	run := pm.Run
	if run == nil {
		return fmt.Errorf("PM2Monitor requires a CommandRunner")
	}

	readFile := pm.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	interval := pm.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	// Build set of watched pm2 app names
	watchedUnits := make(map[string]Target, len(targets))
	for _, t := range targets {
		watchedUnits[t.EffectiveUnit()] = t
	}

	// Track previous restart counts
	prevRestarts := make(map[string]int)

	// Seed initial state
	out, err := run("pm2", "jlist")
	if err == nil {
		for _, p := range pm.parseProcesses(out) {
			if _, ok := watchedUnits[p.Name]; ok {
				prevRestarts[p.Name] = p.PM2Env.RestartTime
			}
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			out, err := run("pm2", "jlist")
			if err != nil {
				continue
			}

			for _, p := range pm.parseProcesses(out) {
				t, ok := watchedUnits[p.Name]
				if !ok {
					continue
				}

				prevCount, hasPrev := prevRestarts[p.Name]
				if hasPrev && p.PM2Env.RestartTime > prevCount {
					// Capture error logs
					preLogs := pm.captureErrorLog(readFile, p.Name)

					now := time.Now()
					inc := Incident{
						ID:           GenerateIncidentID(t.Container, now),
						Container:    t.Container,
						DetectedAt:   now,
						RestartCount: p.PM2Env.RestartTime,
						PrevStarted:  fmt.Sprintf("restart_time was %d", prevCount),
						CurrStarted:  fmt.Sprintf("restart_time is %d", p.PM2Env.RestartTime),
						PreLogs:      preLogs,
						PostLogs:     fmt.Sprintf("status=%s", p.PM2Env.Status),
					}
					if pm.Dir != "" {
						if err := SaveIncident(pm.Dir, &inc); err != nil {
							fmt.Fprintf(os.Stderr, "[pm2-monitor] warning: save incident: %v\n", err)
						}
					}
					select {
					case incidents <- inc:
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				prevRestarts[p.Name] = p.PM2Env.RestartTime
			}
		}
	}
}

// captureErrorLog reads the last 100 lines of the pm2 error log.
// It uses a tail-from-end approach to avoid reading the entire file into memory.
func (pm *PM2Monitor) captureErrorLog(readFile func(string) ([]byte, error), name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "(cannot determine home dir)"
	}
	logPath := filepath.Join(home, ".pm2", "logs", name+"-error.log")

	// Try tail approach first (only for real files, not injected readFile)
	if readFile == nil {
		readFile = os.ReadFile
	}

	data, err := tailFile(logPath, 100)
	if err != nil {
		// Fall back to readFile for testability
		raw, readErr := readFile(logPath)
		if readErr != nil {
			return fmt.Sprintf("(cannot read error log: %v)", readErr)
		}
		lines := strings.Split(string(raw), "\n")
		if len(lines) > 100 {
			lines = lines[len(lines)-100:]
		}
		return strings.Join(lines, "\n")
	}
	return data
}

// tailFile reads the last n lines from a file by seeking from the end.
func tailFile(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}

	size := stat.Size()
	if size == 0 {
		return "", nil
	}

	// Read from end in chunks to find last n newlines
	const chunkSize = 8192
	buf := make([]byte, 0, chunkSize)
	newlines := 0
	offset := size

	for offset > 0 && newlines <= n {
		readSize := int64(chunkSize)
		if readSize > offset {
			readSize = offset
		}
		offset -= readSize

		chunk := make([]byte, readSize)
		_, err := f.ReadAt(chunk, offset)
		if err != nil {
			return "", err
		}
		buf = append(chunk, buf...)

		for _, b := range chunk {
			if b == '\n' {
				newlines++
			}
		}
	}

	lines := strings.Split(string(buf), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}
