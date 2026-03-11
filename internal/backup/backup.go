package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

// Mount represents a Docker volume or bind mount.
type Mount struct {
	Type        string `json:"type"`        // "volume" or "bind"
	Name        string `json:"name"`        // volume name or host path
	Source      string `json:"source"`      // host path
	Destination string `json:"destination"` // container path
}

// ServiceInfo holds container and mount info for a compose service.
type ServiceInfo struct {
	Name      string  `json:"name"`
	Container string  `json:"container"`
	Image     string  `json:"image"`
	Mounts    []Mount `json:"mounts"`
}

// Manifest describes a backup archive.
type Manifest struct {
	Version   string        `json:"version"`
	CreatedAt string        `json:"created_at"`
	Services  []ServiceInfo `json:"services"`
}

// BackupResult is returned after a successful backup.
type BackupResult struct {
	Archive  string   `json:"archive"`
	Services []string `json:"services"`
	Volumes  int      `json:"volumes"`
	Size     string   `json:"size"`
}

// ListEntry represents a single backup in the list.
type ListEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      string `json:"size"`
	CreatedAt string `json:"created_at"`
}

// ComposeProject represents a docker compose project from `docker compose ls`.
type ComposeProject struct {
	Name       string `json:"Name"`
	Status     string `json:"Status"`
	ConfigFile string `json:"ConfigFiles"`
}

// Run performs a backup of all (or filtered) Docker services.
func Run(backupDir, service string) (*BackupResult, error) {
	projects, err := listComposeProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to list compose projects: %w", err)
	}

	if len(projects) == 0 {
		return nil, fmt.Errorf("no docker compose projects found")
	}

	// Create timestamped backup directory
	stamp := time.Now().Format("2006-01-02_1504")
	workDir := filepath.Join(backupDir, fmt.Sprintf("backup_%s", stamp))
	volDir := filepath.Join(workDir, "volumes")
	composeDir := filepath.Join(workDir, "compose")

	if err := os.MkdirAll(volDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create backup dir: %w", err)
	}
	if err := os.MkdirAll(composeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create compose dir: %w", err)
	}

	var allServices []ServiceInfo

	for _, proj := range projects {
		services, err := inspectProject(proj, service)
		if err != nil {
			return nil, err
		}
		allServices = append(allServices, services...)

		// Copy compose files
		copyComposeFiles(proj.ConfigFile, composeDir)
	}

	if len(allServices) == 0 {
		// Clean up empty dirs
		os.RemoveAll(workDir)
		if service != "" {
			return nil, fmt.Errorf("service %q not found in any compose project", service)
		}
		return nil, fmt.Errorf("no services found to back up")
	}

	// Backup volumes
	volumeCount := 0
	for _, svc := range allServices {
		for _, m := range svc.Mounts {
			if err := backupMount(m, volDir); err != nil {
				return nil, fmt.Errorf("failed to backup mount %s: %w", m.Name, err)
			}
			volumeCount++
		}
	}

	// Write manifest
	manifest := Manifest{
		Version:   "1",
		CreatedAt: time.Now().Format(time.RFC3339),
		Services:  allServices,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "manifest.json"), manifestData, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	// Create final tar.gz archive
	archivePath := workDir + ".tar.gz"
	_, err = util.RunCmd("tar", "czf", archivePath, "-C", backupDir, filepath.Base(workDir))
	if err != nil {
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}

	// Remove work directory, keep only the archive
	os.RemoveAll(workDir)

	// Get archive size
	info, err := os.Stat(archivePath)
	size := "unknown"
	if err == nil {
		size = formatSize(info.Size())
	}

	svcNames := make([]string, len(allServices))
	for i, s := range allServices {
		svcNames[i] = s.Name
	}

	return &BackupResult{
		Archive:  archivePath,
		Services: svcNames,
		Volumes:  volumeCount,
		Size:     size,
	}, nil
}

// List returns all backups in the backup directory.
func List(backupDir string) ([]ListEntry, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ListEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []ListEntry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		backups = append(backups, ListEntry{
			Name:      e.Name(),
			Path:      filepath.Join(backupDir, e.Name()),
			Size:      formatSize(info.Size()),
			CreatedAt: info.ModTime().Format(time.RFC3339),
		})
	}
	return backups, nil
}

// listComposeProjects discovers running compose projects.
func listComposeProjects() ([]ComposeProject, error) {
	out, err := util.RunCmd("docker", "compose", "ls", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("docker compose ls failed: %w", err)
	}
	if out == "" {
		return nil, nil
	}

	var projects []ComposeProject
	if err := json.Unmarshal([]byte(out), &projects); err != nil {
		return nil, fmt.Errorf("failed to parse compose ls output: %w", err)
	}
	return projects, nil
}

// inspectProject discovers services and mounts for a compose project.
func inspectProject(proj ComposeProject, filterService string) ([]ServiceInfo, error) {
	// Get container IDs for this project
	out, err := util.RunCmd("docker", "compose", "-p", proj.Name, "ps", "-q")
	if err != nil {
		return nil, fmt.Errorf("failed to list containers for project %s: %w", proj.Name, err)
	}
	if out == "" {
		return nil, nil
	}

	containerIDs := strings.Split(strings.TrimSpace(out), "\n")

	var services []ServiceInfo
	for _, cid := range containerIDs {
		cid = strings.TrimSpace(cid)
		if cid == "" {
			continue
		}

		svc, err := inspectContainer(cid)
		if err != nil {
			continue // skip containers we can't inspect
		}

		if filterService != "" && svc.Name != filterService {
			continue
		}

		services = append(services, *svc)
	}
	return services, nil
}

// dockerInspectResult is the subset of docker inspect JSON we need.
type dockerInspectResult struct {
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

// inspectContainer returns service info from docker inspect.
func inspectContainer(containerID string) (*ServiceInfo, error) {
	out, err := util.RunCmd("docker", "inspect", containerID)
	if err != nil {
		return nil, fmt.Errorf("docker inspect failed for %s: %w", containerID, err)
	}

	var results []dockerInspectResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		return nil, fmt.Errorf("failed to parse docker inspect: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no inspect data for %s", containerID)
	}

	r := results[0]

	// Use compose service label for the name
	name := r.Config.Labels["com.docker.compose.service"]
	if name == "" {
		name = strings.TrimPrefix(r.Name, "/")
	}

	var mounts []Mount
	for _, m := range r.Mounts {
		mt := Mount{
			Type:        m.Type,
			Source:      m.Source,
			Destination: m.Destination,
		}
		if m.Type == "volume" {
			mt.Name = m.Name
		} else {
			mt.Name = m.Source
		}
		mounts = append(mounts, mt)
	}

	return &ServiceInfo{
		Name:      name,
		Container: containerID,
		Image:     r.Config.Image,
		Mounts:    mounts,
	}, nil
}

// backupMount backs up a single mount (volume or bind) to the destination directory.
func backupMount(m Mount, destDir string) error {
	// Sanitize the archive name
	safeName := sanitizeName(m.Name)
	archiveName := safeName + ".tar.gz"

	switch m.Type {
	case "volume":
		// Named volume: use docker run alpine tar pattern
		_, err := util.RunCmd("docker", "run", "--rm",
			"-v", m.Name+":/source:ro",
			"-v", destDir+":/backup",
			"alpine",
			"tar", "czf", "/backup/"+archiveName, "-C", "/source", ".")
		if err != nil {
			return fmt.Errorf("failed to backup volume %s: %w", m.Name, err)
		}
	case "bind":
		// Bind mount: tar directly from host path
		if _, err := os.Stat(m.Source); err != nil {
			return fmt.Errorf("bind mount source %s not accessible: %w", m.Source, err)
		}
		_, err := util.RunCmd("tar", "czf", filepath.Join(destDir, archiveName), "-C", m.Source, ".")
		if err != nil {
			return fmt.Errorf("failed to backup bind mount %s: %w", m.Source, err)
		}
	default:
		// Skip unknown mount types (tmpfs, etc.)
		return nil
	}
	return nil
}

// copyComposeFiles copies compose config files to the backup.
func copyComposeFiles(configFiles, destDir string) {
	for _, f := range strings.Split(configFiles, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		os.WriteFile(filepath.Join(destDir, filepath.Base(f)), data, 0o644)

		// Also try to copy .env from same directory
		envFile := filepath.Join(filepath.Dir(f), ".env")
		if envData, err := os.ReadFile(envFile); err == nil {
			os.WriteFile(filepath.Join(destDir, ".env"), envData, 0o644)
		}
	}
}

// sanitizeName replaces path separators and special chars with underscores.
func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	s := r.Replace(name)
	// Remove leading underscores
	s = strings.TrimLeft(s, "_")
	if s == "" {
		s = "unnamed"
	}
	return s
}

// formatSize formats bytes into human-readable format.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
