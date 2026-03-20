package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Higangssh/homebutler/internal/util"
)

// installedApp tracks where an app is installed.
type installedApp struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Port string `json:"port"`
}

// registryFile returns the path to the installed apps registry.
func registryFile() string {
	return filepath.Join(BaseDir(), "installed.json")
}

// saveInstalled records an app's install location.
func saveInstalled(app installedApp) error {
	all := loadInstalled()
	all[app.Name] = app

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(BaseDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(registryFile(), data, 0644)
}

// removeInstalled removes an app from the registry.
func removeInstalled(appName string) error {
	all := loadInstalled()
	delete(all, appName)

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(registryFile(), data, 0644)
}

// loadInstalled reads the installed apps registry.
func loadInstalled() map[string]installedApp {
	data, err := os.ReadFile(registryFile())
	if err != nil {
		return make(map[string]installedApp)
	}
	var apps map[string]installedApp
	if err := json.Unmarshal(data, &apps); err != nil {
		return make(map[string]installedApp)
	}
	return apps
}

// GetInstalledPath returns the actual install path for an app.
func GetInstalledPath(appName string) string {
	all := loadInstalled()
	if app, ok := all[appName]; ok {
		return app.Path
	}
	// fallback to default
	return AppDir(appName)
}

// App defines a self-hosted application that can be installed.
type App struct {
	Name          string
	Description   string
	ComposeFile   string // go template for docker-compose.yml
	DefaultPort   string // default host port
	ContainerPort string // container port (fixed)
	DataPath      string // container data path
}

// InstallOptions allows user customization of defaults.
type InstallOptions struct {
	Port string // custom host port
}

// composeContext is passed to the compose template.
type composeContext struct {
	Port    string
	DataDir string
	UID     int
	GID     int
}

// Registry holds all installable apps.
var Registry = map[string]App{
	"uptime-kuma": {
		Name:          "uptime-kuma",
		Description:   "A self-hosted monitoring tool",
		DefaultPort:   "3001",
		ContainerPort: "3001",
		DataPath:      "/app/data",
		ComposeFile: `services:
  uptime-kuma:
    image: louislam/uptime-kuma:1
    container_name: uptime-kuma
    restart: unless-stopped
    ports:
      - "{{.Port}}:3001"
    volumes:
      - "{{.DataDir}}:/app/data"
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
`,
	},
	"vaultwarden": {
		Name:          "vaultwarden",
		Description:   "Lightweight Bitwarden-compatible password manager",
		DefaultPort:   "8080",
		ContainerPort: "80",
		DataPath:      "/data",
		ComposeFile: `services:
  vaultwarden:
    image: vaultwarden/server:latest
    container_name: vaultwarden
    restart: unless-stopped
    ports:
      - "{{.Port}}:80"
    volumes:
      - "{{.DataDir}}:/data"
`,
	},
}

// List returns all available apps.
func List() []App {
	apps := make([]App, 0, len(Registry))
	for _, app := range Registry {
		apps = append(apps, app)
	}
	return apps
}

// BaseDir returns the base directory for homebutler apps.
func BaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".homebutler", "apps")
}

// AppDir returns the directory for a specific app.
func AppDir(appName string) string {
	return filepath.Join(BaseDir(), appName)
}

// PreCheck verifies the system is ready for installation.
func PreCheck(app App, port string) []string {
	var issues []string

	// Check docker binary exists
	out, err := util.RunCmd("docker", "--version")
	if err != nil || !strings.Contains(out, "Docker") {
		issues = append(issues, "docker is not installed.\n"+
			"    Install: https://docs.docker.com/engine/install/")
		return issues
	}

	// Check docker daemon is running
	if _, err := util.DockerCmd("info"); err != nil {
		issues = append(issues, "docker daemon is not running.\n"+
			"    Try: sudo systemctl start docker   (Linux)\n"+
			"         colima start                   (macOS)")
		return issues
	}

	// Check docker compose is available
	if _, err := util.DockerCmd("compose", "version"); err != nil {
		issues = append(issues, "docker compose is not available.\n"+
			"    Install: https://docs.docker.com/compose/install/")
		return issues
	}

	// Check port availability
	if isPortInUse(port) {
		issues = append(issues, fmt.Sprintf("port %s is already in use.\n"+
			"    Use --port <number> to pick a different port", port))
	}

	// Check if already running
	composeFile := filepath.Join(GetInstalledPath(app.Name), "docker-compose.yml")
	if _, err := os.Stat(composeFile); err == nil {
		// Compose file exists — check if containers are actually running
		stateOut, _ := util.DockerCmd("compose", "-f", composeFile, "ps", "--format", "{{.State}}")
		if strings.TrimSpace(stateOut) == "running" {
			issues = append(issues, fmt.Sprintf("%s is already running.\n"+
				"    Run: homebutler install uninstall %s", app.Name, app.Name))
		}
	}

	return issues
}

// isPortInUse checks if a port is in use (cross-platform).
func isPortInUse(port string) bool {
	// Try ss (Linux)
	out, err := util.RunCmd("sh", "-c",
		fmt.Sprintf("ss -tlnp 2>/dev/null | grep ':%s ' || true", port))
	if err == nil && out != "" {
		return true
	}

	// Try lsof (macOS/Linux fallback)
	out, err = util.RunCmd("sh", "-c",
		fmt.Sprintf("lsof -i :%s -sTCP:LISTEN 2>/dev/null | grep LISTEN || true", port))
	if err == nil && out != "" {
		return true
	}

	return false
}

// Install creates the app directory, renders docker-compose.yml, and runs it.
func Install(app App, opts InstallOptions) error {
	port := app.DefaultPort
	if opts.Port != "" {
		port = opts.Port
	}

	appDir := AppDir(app.Name)
	dataDir := filepath.Join(appDir, "data")

	// Create directories
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dataDir, err)
	}

	// Render docker-compose.yml
	ctx := composeContext{
		Port:    port,
		DataDir: dataDir,
		UID:     os.Getuid(),
		GID:     os.Getgid(),
	}

	tmpl, err := template.New("compose").Parse(app.ComposeFile)
	if err != nil {
		return fmt.Errorf("invalid compose template: %w", err)
	}

	composeFile := filepath.Join(appDir, "docker-compose.yml")
	f, err := os.Create(composeFile)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", composeFile, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, ctx); err != nil {
		return fmt.Errorf("failed to render compose file: %w", err)
	}

	// Run docker compose up
	_, err = util.DockerCmd("compose", "-f", composeFile, "up", "-d")
	if err != nil {
		return fmt.Errorf("failed to start %s: %w", app.Name, err)
	}

	// Record install location
	return saveInstalled(installedApp{
		Name: app.Name,
		Path: appDir,
		Port: port,
	})
}

// Uninstall stops the app and removes its containers.
func Uninstall(appName string) error {
	appDir := GetInstalledPath(appName)
	composeFile := filepath.Join(appDir, "docker-compose.yml")

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("%s is not installed", appName)
	}

	// docker compose down
	if _, err := util.DockerCmd("compose", "-f", composeFile, "down"); err != nil {
		return fmt.Errorf("failed to stop %s: %w", appName, err)
	}

	return nil
}

// Purge removes the app directory including data.
func Purge(appName string) error {
	appDir := GetInstalledPath(appName)
	if err := Uninstall(appName); err != nil {
		return err
	}
	if err := removeInstalled(appName); err != nil {
		return err
	}
	// Try normal remove first
	err := os.RemoveAll(appDir)
	if err != nil {
		// Docker may create files as root — try passwordless sudo
		_, sudoErr := util.RunCmd("sudo", "-n", "rm", "-rf", appDir)
		if sudoErr != nil {
			return fmt.Errorf("permission denied. Docker creates files as root.\n"+
				"    Run: sudo rm -rf %s", appDir)
		}
	}
	return nil
}

// Status checks if the installed app is running.
func Status(appName string) (string, error) {
	appDir := GetInstalledPath(appName)
	composeFile := filepath.Join(appDir, "docker-compose.yml")

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return "", fmt.Errorf("%s is not installed", appName)
	}

	out, err := util.DockerCmd("compose", "-f", composeFile, "ps",
		"--format", "{{.State}}")
	if err != nil {
		return "", fmt.Errorf("failed to check status: %w", err)
	}
	state := strings.TrimSpace(out)
	if state == "" {
		return "stopped", nil
	}
	return state, nil
}
