package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Higangssh/homebutler/internal/util"
)

// RestoreResult is returned after a successful restore.
type RestoreResult struct {
	Archive  string   `json:"archive"`
	Services []string `json:"services"`
	Volumes  int      `json:"volumes"`
}

// Restore extracts an archive and restores volumes.
func Restore(archivePath, filterService string) (*RestoreResult, error) {
	if _, err := os.Stat(archivePath); err != nil {
		return nil, fmt.Errorf("archive not found: %s", archivePath)
	}

	// Create temp dir for extraction
	tmpDir, err := os.MkdirTemp("", "homebutler-restore-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract archive
	_, err = util.RunCmd("tar", "xzf", archivePath, "-C", tmpDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// Find the backup directory inside the extracted content
	extractedDir, err := findExtractedDir(tmpDir)
	if err != nil {
		return nil, err
	}

	// Read manifest
	manifestPath := filepath.Join(extractedDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("manifest.json not found in archive: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	volDir := filepath.Join(extractedDir, "volumes")
	volumeCount := 0
	var restoredServices []string

	for _, svc := range manifest.Services {
		if filterService != "" && svc.Name != filterService {
			continue
		}
		restoredServices = append(restoredServices, svc.Name)

		for _, m := range svc.Mounts {
			if err := restoreMount(m, volDir); err != nil {
				return nil, fmt.Errorf("failed to restore mount %s: %w", m.Name, err)
			}
			volumeCount++
		}
	}

	if filterService != "" && len(restoredServices) == 0 {
		return nil, fmt.Errorf("service %q not found in backup archive", filterService)
	}

	return &RestoreResult{
		Archive:  archivePath,
		Services: restoredServices,
		Volumes:  volumeCount,
	}, nil
}

// findExtractedDir locates the backup_* directory inside the temp extraction dir.
func findExtractedDir(tmpDir string) (string, error) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", fmt.Errorf("failed to read extracted dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(tmpDir, e.Name()), nil
		}
	}
	// If no subdirectory, the content is directly in tmpDir
	if _, err := os.Stat(filepath.Join(tmpDir, "manifest.json")); err == nil {
		return tmpDir, nil
	}
	return "", fmt.Errorf("invalid archive structure: no backup directory found")
}

// restoreMount restores a single mount from a backup archive.
func restoreMount(m Mount, volDir string) error {
	safeName := sanitizeName(m.Name)
	archivePath := filepath.Join(volDir, safeName+".tar.gz")

	if _, err := os.Stat(archivePath); err != nil {
		return nil // skip if archive doesn't exist (e.g., empty mount)
	}

	switch m.Type {
	case "volume":
		// Restore named volume using docker run alpine tar pattern
		_, err := util.RunCmd("docker", "run", "--rm",
			"-v", m.Name+":/target",
			"-v", volDir+":/backup:ro",
			"alpine",
			"sh", "-c", "cd /target && tar xzf /backup/"+safeName+".tar.gz")
		if err != nil {
			return fmt.Errorf("failed to restore volume %s: %w", m.Name, err)
		}
	case "bind":
		// Restore bind mount to host path
		if err := os.MkdirAll(m.Source, 0o755); err != nil {
			return fmt.Errorf("failed to create bind mount dir %s: %w", m.Source, err)
		}
		_, err := util.RunCmd("tar", "xzf", archivePath, "-C", m.Source)
		if err != nil {
			return fmt.Errorf("failed to restore bind mount %s: %w", m.Source, err)
		}
	}
	return nil
}
