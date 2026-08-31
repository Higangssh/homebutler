package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeArchives creates n archives, oldest first, each of the given size.
func writeArchives(t *testing.T, dir string, sizes []int) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var names []string
	for i, size := range sizes {
		name := filepath.Join(dir, "backup_"+string(rune('a'+i))+".tar.gz")
		if err := os.WriteFile(name, make([]byte, size), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		stamp := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		names = append(names, filepath.Base(name))
	}
	return names
}

func remaining(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestParseByteSize(t *testing.T) {
	cases := map[string]int64{
		"":      0,
		"1024":  1024,
		"1KB":   1 << 10,
		"20GB":  20 << 30,
		"20 GB": 20 << 30,
		"20gb":  20 << 30,
		"1.5GB": 1610612736,
		"2TiB":  2 << 40,
		"500MB": 500 << 20,
		"7G":    7 << 30,
	}
	for in, want := range cases {
		got, err := ParseByteSize(in)
		if err != nil {
			t.Errorf("ParseByteSize(%q) error = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", in, got, want)
		}
	}
	for _, bad := range []string{"twenty", "20 gigs", "-5GB", "GB"} {
		if _, err := ParseByteSize(bad); err == nil {
			t.Errorf("ParseByteSize(%q) accepted a value it cannot mean", bad)
		}
	}
}

// The default is that nothing is ever deleted. A backup can be the last copy of
// something, so retention is opted into rather than out of.
func TestPrune_UnconfiguredKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	writeArchives(t, dir, []int{100, 100, 100, 100, 100})

	cfg := RetentionConfig{}
	if !cfg.IsZero() {
		t.Fatal("an empty RetentionConfig should read as unconfigured")
	}
	res, err := Prune(dir, cfg)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("removed %v with no policy configured", res.Removed)
	}
	if got := len(remaining(t, dir)); got != 5 {
		t.Errorf("%d archives left, want 5", got)
	}
}

func TestPrune_ByCountKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	names := writeArchives(t, dir, []int{10, 10, 10, 10, 10})

	res, err := Prune(dir, RetentionConfig{MaxArchives: 2})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	left := remaining(t, dir)
	if len(left) != 2 {
		t.Fatalf("%d archives left, want 2: %v", len(left), left)
	}
	// The two newest are the last two written.
	for _, want := range names[3:] {
		found := false
		for _, l := range left {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s should have been kept; left = %v", want, left)
		}
	}
	if res.Kept != 2 || len(res.Removed) != 3 {
		t.Errorf("result = %+v, want 2 kept and 3 removed", res)
	}
}

func TestPrune_ByTotalSize(t *testing.T) {
	dir := t.TempDir()
	writeArchives(t, dir, []int{1000, 1000, 1000, 1000})

	// Room for two, not three.
	res, err := Prune(dir, RetentionConfig{MaxBytes: "2500"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Kept != 2 {
		t.Errorf("kept %d, want 2", res.Kept)
	}
	if res.KeptSum > 2500 {
		t.Errorf("kept %d bytes, over the 2500 limit", res.KeptSum)
	}
	if res.Freed != 2000 {
		t.Errorf("freed %d, want 2000", res.Freed)
	}
}

// A limit smaller than a single archive must not empty the directory. Whatever
// the operator meant by it, they did not mean "leave me nothing to restore".
func TestPrune_NeverDeletesTheNewestArchive(t *testing.T) {
	dir := t.TempDir()
	writeArchives(t, dir, []int{5000, 5000, 9999})

	res, err := Prune(dir, RetentionConfig{MaxBytes: "1KB"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	left := remaining(t, dir)
	if len(left) != 1 {
		t.Fatalf("%d archives left, want exactly 1: %v", len(left), left)
	}
	if res.Kept != 1 {
		t.Errorf("kept = %d, want 1", res.Kept)
	}
	// And it is the newest one, not whichever happened to fit.
	if left[0] != "backup_c.tar.gz" {
		t.Errorf("kept %s, want the newest archive", left[0])
	}
}

func TestPrune_CountAndSizeTogetherTakeTheStricter(t *testing.T) {
	dir := t.TempDir()
	writeArchives(t, dir, []int{1000, 1000, 1000, 1000, 1000})

	res, err := Prune(dir, RetentionConfig{MaxArchives: 4, MaxBytes: "2000"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Kept != 2 {
		t.Errorf("kept %d, want 2 — the size limit is stricter than the count", res.Kept)
	}
}

// Retention must not touch anything it did not create.
func TestPrune_IgnoresFilesThatAreNotArchives(t *testing.T) {
	dir := t.TempDir()
	writeArchives(t, dir, []int{10, 10, 10})
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Prune(dir, RetentionConfig{MaxArchives: 1}); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a file that is not a backup was removed: %v", err)
	}
}

func TestPrune_RejectsAnUnparseableSize(t *testing.T) {
	if _, err := Prune(t.TempDir(), RetentionConfig{MaxBytes: "twenty gigs"}); err == nil {
		t.Error("Prune() accepted a size it cannot mean")
	}
}

func TestDirUsage(t *testing.T) {
	dir := t.TempDir()
	writeArchives(t, dir, []int{100, 250})
	count, total, err := DirUsage(dir)
	if err != nil {
		t.Fatalf("DirUsage() error = %v", err)
	}
	if count != 2 || total != 350 {
		t.Errorf("DirUsage() = %d, %d; want 2, 350", count, total)
	}

	missing, sum, err := DirUsage(filepath.Join(dir, "nope"))
	if err != nil || missing != 0 || sum != 0 {
		t.Errorf("a missing directory should read as empty, got %d, %d, %v", missing, sum, err)
	}
}

// The archive name used to stop at the minute, so two backups started inside
// the same minute wrote to the same path and the second replaced the first —
// both reporting success. Retention counts archives, so several runs collapsing
// into one also quietly changed what a count limit meant.
func TestArchiveStampSeparatesRapidBackups(t *testing.T) {
	base := time.Date(2026, 8, 30, 22, 36, 12, 0, time.UTC)
	// A small project backs up fast enough that consecutive runs land in the
	// same second, so seconds were not enough either.
	for _, gap := range []time.Duration{35 * time.Second, 400 * time.Millisecond} {
		first := base.Format(archiveStampLayout)
		second := base.Add(gap).Format(archiveStampLayout)
		if first == second {
			t.Errorf("two times %s apart produced the same stamp %q", gap, first)
		}
	}
}
