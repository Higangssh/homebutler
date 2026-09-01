package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Higangssh/homebutler/internal/util"
)

// RestoreResult is returned after a successful restore.
type RestoreResult struct {
	Archive  string   `json:"archive"`
	Services []string `json:"services"`
	Volumes  int      `json:"volumes"`
	// Refused lists mounts the archive asked for and restore declined to
	// perform. A refusal is never silent: an operator who sees fewer volumes
	// than expected must be able to find out why from the result alone.
	Refused []RefusedMount `json:"refused,omitempty"`
}

// RefusedMount records one mount that was declined, and why.
type RefusedMount struct {
	Service string `json:"service"`
	Type    string `json:"type"`
	Target  string `json:"target"`
	Reason  string `json:"reason"`
}

// RestoreOptions carries what the operator asked for, as distinct from what
// the archive declares. Every filesystem target restore writes to has to be
// traceable to this struct rather than to manifest.json.
type RestoreOptions struct {
	// Service restores only the named service when set.
	Service string
	// AllowBind lists host paths the operator has explicitly permitted as
	// bind-mount targets. Empty means no bind mount is restored.
	AllowBind []string
}

// dockerVolumeName is docker's own volume name grammar. A manifest value that
// does not match is not naming a volume; it is naming a path, and
// `docker run -v <path>:/target` would bind-mount that host path into a
// container the daemon runs as root.
var dockerVolumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// resolveBindTarget returns the real path target refers to, and an error when
// that path is not inside a root the operator permitted.
//
// A lexical prefix check is not enough, and the reason is that one archive can
// contain two bind mounts. The first extracts normally inside the allowed root
// and its payload plants a symlink there; the second names that symlink as its
// source. Lexically the second is inside the allowed root, so it passes —
// os.MkdirAll follows the symlink, finds a directory and does nothing, and
// `tar -C` follows it too and writes wherever it points. That defeats the
// containment entirely, and neither mount looks unusual on its own.
//
// So the check is made against the resolved path, and it is made per mount at
// the point the mount is about to be restored, because the symlink that breaks
// it does not exist until an earlier mount in the same archive has created it.
func resolveBindTarget(target string, allowed []string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", target, err)
	}
	abs = filepath.Clean(abs)

	// The deepest part of the path that exists is the most that can be
	// resolved; the rest is what restore is about to create, and a path that
	// does not exist yet cannot be a symlink.
	existing := abs
	var pending []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		pending = append([]string{filepath.Base(existing)}, pending...)
		existing = parent
	}
	resolvedBase, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", target, err)
	}
	resolved := filepath.Join(append([]string{resolvedBase}, pending...)...)

	for _, root := range allowed {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		// The permitted root is resolved too: an operator who names a path that
		// is itself a symlink meant the place it points to.
		rootResolved, err := filepath.EvalSymlinks(filepath.Clean(rootAbs))
		if err != nil {
			rootResolved = filepath.Clean(rootAbs)
		}
		if resolved == rootResolved || strings.HasPrefix(resolved, rootResolved+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	if resolved != abs {
		return "", fmt.Errorf("bind target %s resolves to %s, which is outside every permitted root", target, resolved)
	}
	return "", fmt.Errorf("bind target %s was not permitted; pass --allow-bind %s to restore it", target, target)
}

// Restore extracts an archive and restores volumes.
//
// Paths declared in manifest.json are attacker-controlled whenever the archive
// came from somewhere else, which is the normal case for a tool built around
// portable backups. Nothing in the manifest selects a filesystem target on its
// own: volume names must look like volume names, and bind targets must have
// been named by the operator in opts.AllowBind.
func Restore(archivePath string, opts RestoreOptions) (*RestoreResult, error) {
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
	if err := extractTarGz(archivePath, tmpDir); err != nil {
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

	var refused []RefusedMount

	for _, svc := range manifest.Services {
		if opts.Service != "" && svc.Name != opts.Service {
			continue
		}
		restoredServices = append(restoredServices, svc.Name)

		for _, m := range svc.Mounts {
			// A mount with no payload is only refused in appearance: there is
			// nothing to write, so restoreMount no-ops on it either way. Let it
			// fall through so the volume count keeps its existing meaning.
			if mountHasPayload(m, volDir) {
				// Cleared per mount, in order, immediately before the mount is
				// written: an earlier mount in this same archive can have
				// created the symlink that would let this one escape.
				cleared, reason := refuseMount(m, opts)
				if reason != "" {
					refused = append(refused, RefusedMount{
						Service: svc.Name,
						Type:    m.Type,
						Target:  mountTarget(m),
						Reason:  reason,
					})
					continue
				}
				m = cleared
			}
			if err := restoreMount(m, volDir, archivePath); err != nil {
				return nil, fmt.Errorf("failed to restore mount %s: %w", m.Name, err)
			}
			volumeCount++
		}
	}

	if opts.Service != "" && len(restoredServices) == 0 {
		return nil, fmt.Errorf("service %q not found in backup archive", opts.Service)
	}

	return &RestoreResult{
		Refused:  refused,
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

// mountHasPayload reports whether the archive actually carries data for this
// mount. backupMount writes nothing for a bind source that did not exist, so a
// manifest can name mounts that restore has nothing to write. Those are not
// worth refusing: refusing them would report a permission problem where there
// is only an empty mount.
func mountHasPayload(m Mount, volDir string) bool {
	_, err := os.Stat(mountArchivePath(m.Name, volDir))
	return err == nil
}

// mountArchivePath is where a mount's payload lives inside an extracted backup.
//
// It has one definition because the containment checks are only correct while
// every caller agrees which archive a mount refers to. mountHasPayload decides
// whether refuseMount runs at all, and restoreMount decides what to write; if
// those two ever disagreed about the name, a mount could be cleared as empty
// and then restored anyway.
func mountArchivePath(name, volDir string) string {
	return filepath.Join(volDir, sanitizeName(name)+".tar.gz")
}

// mountTarget names what a mount would write to, for reporting a refusal.
func mountTarget(m Mount) string {
	if m.Type == "bind" {
		return m.Source
	}
	return m.Name
}

// refuseMount returns a human-readable reason to decline a mount, or "" to
// allow it. This is the only gate between manifest.json and the filesystem,
// so it runs before restoreMount for every mount, of every type.
func refuseMount(m Mount, opts RestoreOptions) (Mount, string) {
	switch m.Type {
	case "volume":
		// Anything that is not a bare volume name reaches `docker run -v` as a
		// host path, and the daemon mounts it as root.
		if !dockerVolumeName.MatchString(m.Name) {
			return m, fmt.Sprintf("%q is not a Docker volume name; the archive may be trying to select a host path", m.Name)
		}
		return m, ""
	case "bind":
		if m.Source == "" {
			return m, "the archive declares a bind mount with no source path"
		}
		if !filepath.IsAbs(m.Source) {
			return m, fmt.Sprintf("bind target %q is not an absolute path", m.Source)
		}
		resolved, err := resolveBindTarget(m.Source, opts.AllowBind)
		if err != nil {
			return m, err.Error()
		}
		// The caller writes to the resolved path, not the declared one, so the
		// path that was checked is the path that gets extracted into.
		m.Source = resolved
		return m, ""
	default:
		return m, fmt.Sprintf("unknown mount type %q", m.Type)
	}
}

// restoreMount restores a single mount from a backup archive.
//
// Callers must have cleared the mount through refuseMount first; this function
// assumes m.Name is a volume name and m.Source is an operator-permitted path.
func restoreMount(m Mount, volDir, sourceArchive string) error {
	safeName := sanitizeName(m.Name)
	archivePath := mountArchivePath(m.Name, volDir)

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
			if util.IsPermissionError(err) {
				return util.NewHintError("cannot create bind mount dir "+m.Source, err, "sudo homebutler restore "+sourceArchive)
			}
			return fmt.Errorf("failed to create bind mount dir %s: %w", m.Source, err)
		}
		if err := extractTarGz(archivePath, m.Source); err != nil {
			return fmt.Errorf("failed to restore bind mount %s: %w", m.Source, err)
		}
	}
	return nil
}
