package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my_volume", "my_volume"},
		{"/var/lib/data", "var_lib_data"},
		{"volume:name", "volume_name"},
		{"path\\with\\backslash", "path_with_backslash"},
		{" spaces here", "spaces_here"},
		{"///leading", "leading"},
		{"", "unnamed"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{2684354560, "2.5 GB"},
	}
	for _, tt := range tests {
		got := FormatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("List() returned %d entries, want 0", len(entries))
	}
}

func TestListNonExistentDir(t *testing.T) {
	entries, err := List("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("List() should not error on nonexistent dir, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("List() returned %d entries, want 0", len(entries))
	}
}

func TestListWithArchives(t *testing.T) {
	dir := t.TempDir()

	// Create fake tar.gz files
	os.WriteFile(filepath.Join(dir, "backup_2026-03-11_1830.tar.gz"), []byte("fake"), 0o644)
	os.WriteFile(filepath.Join(dir, "backup_2026-03-10_0900.tar.gz"), []byte("fake2"), 0o644)
	os.WriteFile(filepath.Join(dir, "not-a-backup.txt"), []byte("ignore"), 0o644)

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("List() returned %d entries, want 2", len(entries))
	}

	// Verify entries have expected fields
	for _, e := range entries {
		if e.Name == "" {
			t.Error("entry Name is empty")
		}
		if e.Path == "" {
			t.Error("entry Path is empty")
		}
		if e.Size == "" {
			t.Error("entry Size is empty")
		}
	}
}

func TestManifestJSON(t *testing.T) {
	m := Manifest{
		Version:   "1",
		CreatedAt: "2026-03-11T18:30:00Z",
		Services: []ServiceInfo{
			{
				Name:      "postgres",
				Container: "abc123",
				Image:     "postgres:16",
				Mounts: []Mount{
					{Type: "volume", Name: "pg_data", Source: "/var/lib/docker/volumes/pg_data/_data", Destination: "/var/lib/postgresql/data"},
				},
			},
		},
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var parsed Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if parsed.Version != "1" {
		t.Errorf("Version = %q, want %q", parsed.Version, "1")
	}
	if len(parsed.Services) != 1 {
		t.Fatalf("Services count = %d, want 1", len(parsed.Services))
	}
	if parsed.Services[0].Name != "postgres" {
		t.Errorf("Service name = %q, want %q", parsed.Services[0].Name, "postgres")
	}
	if len(parsed.Services[0].Mounts) != 1 {
		t.Fatalf("Mounts count = %d, want 1", len(parsed.Services[0].Mounts))
	}
	if parsed.Services[0].Mounts[0].Type != "volume" {
		t.Errorf("Mount type = %q, want %q", parsed.Services[0].Mounts[0].Type, "volume")
	}
}

func TestFindExtractedDir(t *testing.T) {
	// Case 1: subdirectory exists
	dir := t.TempDir()
	subDir := filepath.Join(dir, "backup_2026-03-11_1830")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(subDir, "manifest.json"), []byte("{}"), 0o644)

	found, err := findExtractedDir(dir)
	if err != nil {
		t.Fatalf("findExtractedDir() error = %v", err)
	}
	if found != subDir {
		t.Errorf("findExtractedDir() = %q, want %q", found, subDir)
	}

	// Case 2: manifest directly in root
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "manifest.json"), []byte("{}"), 0o644)

	found2, err := findExtractedDir(dir2)
	if err != nil {
		t.Fatalf("findExtractedDir() error = %v", err)
	}
	if found2 != dir2 {
		t.Errorf("findExtractedDir() = %q, want %q", found2, dir2)
	}
}

func TestCopyComposeFiles(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create a compose file and .env
	composeFile := filepath.Join(srcDir, "docker-compose.yml")
	os.WriteFile(composeFile, []byte("version: '3'\nservices:\n  web:\n    image: nginx"), 0o644)
	os.WriteFile(filepath.Join(srcDir, ".env"), []byte("FOO=bar"), 0o644)

	copyComposeFiles(composeFile, destDir)

	// Verify compose file was copied
	if _, err := os.Stat(filepath.Join(destDir, "docker-compose.yml")); err != nil {
		t.Error("docker-compose.yml was not copied")
	}

	// Verify .env was copied
	if _, err := os.Stat(filepath.Join(destDir, ".env")); err != nil {
		t.Error(".env was not copied")
	}
}

// #282: a Jellyfin media bind mount can be terabytes, and it went into every
// archive. What the exclusion has to get right is not only "skip it" but
// "skip exactly it".
func TestExcludedByCoversTheMountAndWhatIsUnderIt(t *testing.T) {
	media := Mount{Type: "bind", Source: "/mnt/media", Name: "/mnt/media"}
	movies := Mount{Type: "bind", Source: "/mnt/media/movies", Name: "/mnt/media/movies"}
	config := Mount{Type: "bind", Source: "/srv/jellyfin/config", Name: "/srv/jellyfin/config"}
	// The one a prefix match gets wrong if it does not check the boundary.
	sibling := Mount{Type: "bind", Source: "/mnt/media-backup", Name: "/mnt/media-backup"}

	exclude := []string{"/mnt/media"}

	if _, ok := excludedBy(media, exclude); !ok {
		t.Error("the excluded path itself was not excluded")
	}
	// Somebody who says /mnt/media means the media. A compose file that mounts
	// a subdirectory separately has not changed their mind.
	if _, ok := excludedBy(movies, exclude); !ok {
		t.Error("a mount under the excluded path was not excluded")
	}
	if _, ok := excludedBy(config, exclude); ok {
		t.Error("an unrelated mount was excluded")
	}
	if _, ok := excludedBy(sibling, exclude); ok {
		t.Error("/mnt/media-backup was excluded by /mnt/media: the match is not on a path boundary")
	}
}

// A named volume is Docker's own directory and has no host path to match.
// Matching its name against a path would mostly be a way to exclude nothing
// while appearing to work.
func TestOnlyBindMountsAreExcluded(t *testing.T) {
	volume := Mount{Type: "volume", Name: "jellyfin_config", Source: "/var/lib/docker/volumes/jellyfin_config/_data"}
	if _, ok := excludedBy(volume, []string{"/var/lib/docker/volumes/jellyfin_config/_data"}); ok {
		t.Error("a named volume was excluded by its docker-internal path")
	}
}

func TestExcludePathsAreNormalised(t *testing.T) {
	m := Mount{Type: "bind", Source: "/mnt/media", Name: "/mnt/media"}
	for _, pattern := range []string{"/mnt/media/", "/mnt/media", " /mnt/media ", "/mnt/./media"} {
		if _, ok := excludedBy(m, []string{pattern}); !ok {
			t.Errorf("%q did not match /mnt/media", pattern)
		}
	}
	// An empty exclusion must not match everything, which is what a bare
	// --exclude= would otherwise do. The protection is filepath.Clean, not a
	// guard: Clean("") is ".", and "." does not prefix an absolute path. Used
	// raw the pattern would be "", and "" + "/" prefixes every absolute path
	// there is — which is the break this catches.
	for _, pattern := range []string{"", "  "} {
		if _, ok := excludedBy(m, []string{pattern}); ok {
			t.Errorf("%q excluded a mount", pattern)
		}
	}
}
