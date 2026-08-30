package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// RetentionConfig bounds how much backup history is kept on disk.
//
// Both limits default to unlimited, which is the opposite of the incident
// directory and is deliberate. A pruned incident costs some history; a pruned
// backup can be the only remaining copy of data that no longer exists anywhere
// else. Deleting that is not a default anyone chose, so retention is something
// an operator turns on. What homebutler does without being asked is report the
// size, through `doctor`.
type RetentionConfig struct {
	// MaxArchives is how many archives to keep, newest first. Zero, the
	// default, keeps every archive.
	MaxArchives int `yaml:"max_archives,omitempty" json:"max_archives"`

	// MaxBytes caps the total size of the directory, as a size with a unit —
	// "500MB", "20GB". Empty, the default, means no cap.
	//
	// Count alone is not a disk guarantee, and a disk guarantee is what someone
	// running backup from cron actually wants: ten archives can be 200MB or
	// 200GB depending on what was running when they were taken.
	MaxBytes string `yaml:"max_bytes,omitempty" json:"max_bytes"`
}

// IsZero reports whether retention is unconfigured, so callers can skip the
// work and say nothing rather than reporting a prune that removed nothing.
func (r RetentionConfig) IsZero() bool {
	return r.MaxArchives <= 0 && strings.TrimSpace(r.MaxBytes) == ""
}

// PruneResult describes what a prune removed.
type PruneResult struct {
	Removed []string `json:"removed,omitempty"`
	Freed   int64    `json:"freed_bytes,omitempty"`
	Kept    int      `json:"kept"`
	KeptSum int64    `json:"kept_bytes"`
}

// ParseByteSize reads a size written the way an operator writes one.
//
// Both conventions are accepted for the same reason `df` and every dashboard
// disagree about them: "20GB" and "20GiB" both mean what the person typing
// meant, and refusing one of them teaches nothing.
func ParseByteSize(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, nil
	}
	upper := strings.ToUpper(strings.ReplaceAll(trimmed, " ", ""))

	units := []struct {
		suffix string
		mult   int64
	}{
		{"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if !strings.HasSuffix(upper, u.suffix) {
			continue
		}
		num := strings.TrimSuffix(upper, u.suffix)
		val, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q", s)
		}
		if val < 0 {
			return 0, fmt.Errorf("invalid size %q: negative", s)
		}
		return int64(val * float64(u.mult)), nil
	}

	val, err := strconv.ParseInt(upper, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: want a number and a unit, like \"20GB\"", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("invalid size %q: negative", s)
	}
	return val, nil
}

// archiveFile is one archive on disk, with the size and time prune needs.
type archiveFile struct {
	path    string
	name    string
	size    int64
	modTime int64
}

// listArchiveFiles returns the archives in dir, newest first.
func listArchiveFiles(dir string) ([]archiveFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []archiveFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, archiveFile{
			path:    filepath.Join(dir, e.Name()),
			name:    e.Name(),
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime > files[j].modTime })
	return files, nil
}

// DirUsage reports how many archives dir holds and what they total.
//
// This is the half of the picture `doctor` was missing: it already checks that
// a backup is recent, which says nothing about whether the directory has been
// growing without a bound since the day it was created.
func DirUsage(dir string) (count int, total int64, err error) {
	files, err := listArchiveFiles(dir)
	if err != nil {
		return 0, 0, err
	}
	for _, f := range files {
		total += f.size
	}
	return len(files), total, nil
}

// Prune deletes the oldest archives until dir is inside cfg.
//
// The newest archive is never deleted, whatever the limits say — including when
// it exceeds MaxBytes by itself. A limit that empties the directory has
// misunderstood what it was asked to bound, and the operator is left with
// nothing to restore from.
func Prune(dir string, cfg RetentionConfig) (*PruneResult, error) {
	maxBytes, err := ParseByteSize(cfg.MaxBytes)
	if err != nil {
		return nil, err
	}

	files, err := listArchiveFiles(dir)
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}
	result := &PruneResult{}
	if len(files) == 0 {
		return result, nil
	}

	// Newest first, so the walk keeps what it should and everything after the
	// point a limit is reached is what goes.
	keepUntil := len(files)
	if cfg.MaxArchives > 0 && cfg.MaxArchives < keepUntil {
		keepUntil = cfg.MaxArchives
	}
	if maxBytes > 0 {
		var running int64
		for i := 0; i < keepUntil; i++ {
			running += files[i].size
			if running > maxBytes {
				// The newest archive is kept even when it alone is over.
				keepUntil = i
				if keepUntil == 0 {
					keepUntil = 1
				}
				break
			}
		}
	}
	if keepUntil < 1 {
		keepUntil = 1
	}

	for _, f := range files[keepUntil:] {
		if err := os.Remove(f.path); err != nil {
			return nil, fmt.Errorf("remove old backup %s: %w", f.name, err)
		}
		result.Removed = append(result.Removed, f.name)
		result.Freed += f.size
	}
	for _, f := range files[:keepUntil] {
		result.KeptSum += f.size
	}
	result.Kept = keepUntil
	return result, nil
}
