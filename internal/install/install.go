package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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
	Port     string // custom host port
	MediaDir string // media directory (jellyfin)
	DryRun   bool   // do not actually install, just show what would happen
}

// composeContext is passed to the compose template.
type composeContext struct {
	Port         string
	DataDir      string
	UID          int
	GID          int
	MediaDir     string
	DockerSocket string
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
	"filebrowser": {
		Name:          "filebrowser",
		Description:   "Web-based file manager",
		DefaultPort:   "8081",
		ContainerPort: "80",
		DataPath:      "/srv",
		ComposeFile: `services:
  filebrowser:
    image: filebrowser/filebrowser:latest
    container_name: filebrowser
    restart: unless-stopped
    ports:
      - "{{.Port}}:80"
    volumes:
      - "{{.DataDir}}:/srv"
`,
	},
	"it-tools": {
		Name:          "it-tools",
		Description:   "Collection of developer utilities (JSON, Base64, Hash, etc.)",
		DefaultPort:   "8082",
		ContainerPort: "80",
		DataPath:      "/data",
		ComposeFile: `services:
  it-tools:
    image: corentinth/it-tools:latest
    container_name: it-tools
    restart: unless-stopped
    ports:
      - "{{.Port}}:80"
`,
	},
	"jellyfin": {
		Name:          "jellyfin",
		Description:   "Free software media system for streaming movies, TV, and music",
		DefaultPort:   "8096",
		ContainerPort: "8096",
		DataPath:      "/config",
		ComposeFile: `services:
  jellyfin:
    image: jellyfin/jellyfin:latest
    container_name: jellyfin
    restart: unless-stopped
    ports:
      - "{{.Port}}:8096"
    volumes:
      - "{{.DataDir}}/config:/config"
      - "{{.DataDir}}/cache:/cache"{{if .MediaDir}}
      - "{{.MediaDir}}:/media:ro"{{end}}
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
`,
	},
	"gitea": {
		Name:          "gitea",
		Description:   "Lightweight self-hosted Git service",
		DefaultPort:   "3002",
		ContainerPort: "3000",
		DataPath:      "/data",
		ComposeFile: `services:
  gitea:
    image: gitea/gitea:latest
    container_name: gitea
    restart: unless-stopped
    ports:
      - "{{.Port}}:3000"
      - "2222:22"
    volumes:
      - "{{.DataDir}}:/data"
    environment:
      - USER_UID={{.UID}}
      - USER_GID={{.GID}}
`,
	},
	"homepage": {
		Name:          "homepage",
		Description:   "Modern dashboard for your homelab with service integration",
		DefaultPort:   "3010",
		ContainerPort: "3000",
		DataPath:      "/app/config",
		ComposeFile: `services:
  homepage:
    image: ghcr.io/gethomepage/homepage:latest
    container_name: homepage
    restart: unless-stopped
    ports:
      - "{{.Port}}:3000"
    volumes:
      - "{{.DataDir}}/config:/app/config"
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
`,
	},
	"stirling-pdf": {
		Name:          "stirling-pdf",
		Description:   "All-in-one PDF manipulation tool (merge, split, convert, OCR)",
		DefaultPort:   "8083",
		ContainerPort: "8080",
		DataPath:      "/configs",
		ComposeFile: `services:
  stirling-pdf:
    image: frooodle/s-pdf:latest
    container_name: stirling-pdf
    restart: unless-stopped
    ports:
      - "{{.Port}}:8080"
    volumes:
      - "{{.DataDir}}/configs:/configs"
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
`,
	},
	"speedtest-tracker": {
		Name:          "speedtest-tracker",
		Description:   "Internet speed test tracker with historical data and graphs",
		DefaultPort:   "8084",
		ContainerPort: "80",
		DataPath:      "/config",
		ComposeFile: `services:
  speedtest-tracker:
    image: lscr.io/linuxserver/speedtest-tracker:latest
    container_name: speedtest-tracker
    restart: unless-stopped
    ports:
      - "{{.Port}}:80"
    volumes:
      - "{{.DataDir}}/config:/config"
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
`,
	},
	"mealie": {
		Name:          "mealie",
		Description:   "Self-hosted recipe manager and meal planner",
		DefaultPort:   "9925",
		ContainerPort: "9000",
		DataPath:      "/app/data",
		ComposeFile: `services:
  mealie:
    image: ghcr.io/mealie-recipes/mealie:latest
    container_name: mealie
    restart: unless-stopped
    ports:
      - "{{.Port}}:9000"
    volumes:
      - "{{.DataDir}}/data:/app/data"
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
`,
	},
	"pi-hole": {
		Name:          "pi-hole",
		Description:   "Network-wide ad blocking via DNS filtering",
		DefaultPort:   "8088",
		ContainerPort: "80",
		DataPath:      "/etc/pihole",
		ComposeFile: `services:
  pihole:
    image: pihole/pihole:latest
    container_name: pihole
    restart: unless-stopped
    ports:
      - "{{.Port}}:80"
      - "53:53/tcp"
      - "53:53/udp"
    volumes:
      - "{{.DataDir}}/pihole:/etc/pihole"
      - "{{.DataDir}}/dnsmasq:/etc/dnsmasq.d"
    cap_add:
      - NET_ADMIN
`,
	},
	"adguard-home": {
		Name:          "adguard-home",
		Description:   "DNS-based ad blocker and privacy protection",
		DefaultPort:   "3000",
		ContainerPort: "3000",
		DataPath:      "/opt/adguardhome/work",
		ComposeFile: `services:
  adguard-home:
    image: adguard/adguardhome:latest
    container_name: adguard-home
    restart: unless-stopped
    ports:
      - "{{.Port}}:3000"
      - "53:53/tcp"
      - "53:53/udp"
    volumes:
      - "{{.DataDir}}/work:/opt/adguardhome/work"
      - "{{.DataDir}}/conf:/opt/adguardhome/conf"
`,
	},
	"portainer": {
		Name:          "portainer",
		Description:   "Docker management GUI with container, image, and volume control",
		DefaultPort:   "9443",
		ContainerPort: "9443",
		DataPath:      "/data",
		ComposeFile: `services:
  portainer:
    image: portainer/portainer-ce:latest
    container_name: portainer
    restart: unless-stopped
    ports:
      - "{{.Port}}:9443"
    volumes:
      - "{{.DockerSocket}}:/var/run/docker.sock"
      - "{{.DataDir}}/data:/data"
`,
	},
	"nginx-proxy-manager": {
		Name:          "nginx-proxy-manager",
		Description:   "Reverse proxy with SSL termination and web UI for managing hosts",
		DefaultPort:   "81",
		ContainerPort: "81",
		DataPath:      "/data",
		ComposeFile: `services:
  nginx-proxy-manager:
    image: jc21/nginx-proxy-manager:latest
    container_name: nginx-proxy-manager
    restart: unless-stopped
    ports:
      - "{{.Port}}:81"
      - "80:80"
      - "443:443"
    volumes:
      - "{{.DataDir}}/data:/data"
      - "{{.DataDir}}/letsencrypt:/etc/letsencrypt"
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
// Falls back to /tmp/.homebutler/apps if home directory cannot be determined.
func BaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine home directory: %v, using /tmp fallback\n", err)
		home = "/tmp"
	}
	return filepath.Join(home, ".homebutler", "apps")
}

// AppDir returns the directory for a specific app.
func AppDir(appName string) string {
	return filepath.Join(BaseDir(), appName)
}

// dnsApps are apps that use port 53 for DNS.
var dnsApps = map[string]string{
	"pi-hole":      "adguard-home",
	"adguard-home": "pi-hole",
}

// Testable hooks (overridden in tests).
var (
	checkPortInUse = portInUseBy
	getInstalled   = loadInstalled
)

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
	if processInfo := checkPortInUse(port); processInfo != "" {
		issues = append(issues, fmt.Sprintf("port %s is already in use by %s.\n"+
			"    Use --port <number> to pick a different port", port, processInfo))
	} else {
		// Also check Docker containers (colima/podman may not show in lsof)
		dockerOut, _ := util.DockerCmd("ps", "--format", "{{.Names}} {{.Ports}}")
		if strings.Contains(dockerOut, ":"+port+"->") {
			// Extract container name
			for _, line := range strings.Split(dockerOut, "\n") {
				if strings.Contains(line, ":"+port+"->") {
					parts := strings.Fields(line)
					if len(parts) > 0 {
						issues = append(issues, fmt.Sprintf("port %s is already in use by container %q.\n"+
							"    Use --port <number> to pick a different port", port, parts[0]))
					}
					break
				}
			}
		}
	}

	// DNS apps: check port 53 and mutual conflict
	if conflict, ok := dnsApps[app.Name]; ok {
		// Check port 53
		if processInfo := checkPortInUse("53"); processInfo != "" {
			var msg string
			if runtime.GOOS == "darwin" {
				msg = "Port 53 is in use. Check what's using it: sudo lsof -i :53"
			} else {
				msg = "Port 53 is in use (possibly systemd-resolved). " +
					"Disable it: sudo systemctl disable --now systemd-resolved"
			}
			issues = append(issues, msg)
		}
		// Check if the other DNS app is already installed
		installed := getInstalled()
		if _, exists := installed[conflict]; exists {
			issues = append(issues, fmt.Sprintf("%s is already installed. "+
				"Running two DNS servers is not recommended.", conflict))
		}
	}

	// nginx-proxy-manager: check port 80/443
	if app.Name == "nginx-proxy-manager" {
		var blocked []string
		if processInfo := checkPortInUse("80"); processInfo != "" {
			blocked = append(blocked, "80")
		}
		if processInfo := checkPortInUse("443"); processInfo != "" {
			blocked = append(blocked, "443")
		}
		if len(blocked) > 0 {
			issues = append(issues, fmt.Sprintf("Port %s is in use. "+
				"nginx-proxy-manager needs these ports for reverse proxy.",
				strings.Join(blocked, "/")))
		}
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

// PostInstallMessage returns app-specific guidance shown after successful install.
func PostInstallMessage(appName, port string) string {
	switch appName {
	case "pi-hole":
		return "Set your device/router DNS to this server's IP to enable ad blocking."
	case "adguard-home":
		return fmt.Sprintf("Complete initial setup at http://localhost:%s, then set your DNS.", port)
	case "portainer":
		return "Access via HTTPS at https://localhost:9443 (self-signed cert)."
	case "nginx-proxy-manager":
		return "Default login: admin@example.com / changeme. Change immediately!"
	default:
		return ""
	}
}

// IsSpecialWarning returns a pre-install warning for special apps (shown but doesn't block install).
func IsSpecialWarning(appName string) string {
	if appName == "portainer" {
		return "⚠️  Portainer requires Docker socket access. It will have full control over all containers."
	}
	return ""
}

// isPortInUse checks if a port is in use (cross-platform).
func portInUseBy(port string) string {
	// Try lsof (macOS/Linux) — gives process name
	out, err := util.RunCmd("sh", "-c",
		fmt.Sprintf("lsof -i :%s -sTCP:LISTEN 2>/dev/null | grep LISTEN | head -1 || true", port))
	if err == nil && out != "" {
		fields := strings.Fields(out)
		if len(fields) >= 2 {
			return fmt.Sprintf("%s (PID %s)", fields[0], fields[1])
		}
		return "unknown process"
	}

	// Try ss (Linux)
	out, err = util.RunCmd("sh", "-c",
		fmt.Sprintf("ss -tlnp 2>/dev/null | grep ':%s ' | head -1 || true", port))
	if err == nil && out != "" {
		return "in use"
	}

	return ""
}

func isPortInUse(port string) bool {
	return portInUseBy(port) != ""
}

// ValidatePort checks that a port string is a valid port number (1-65535).
func ValidatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port %q: must be a number", port)
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %d: must be between 1 and 65535", n)
	}
	return nil
}

// Install creates the app directory, renders docker-compose.yml, and runs it.
func Install(app App, opts InstallOptions) error {
	port := app.DefaultPort
	if opts.Port != "" {
		port = opts.Port
	}

	if err := ValidatePort(port); err != nil {
		return err
	}

	appDir := AppDir(app.Name)
	dataDir := filepath.Join(appDir, "data")

	// Create directories
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dataDir, err)
	}

	// Render docker-compose.yml
	ctx := composeContext{
		Port:         port,
		DataDir:      dataDir,
		UID:          os.Getuid(),
		GID:          os.Getgid(),
		MediaDir:     opts.MediaDir,
		DockerSocket: util.DockerSocket(),
	}

	tmpl, err := template.New("compose").Parse(app.ComposeFile)
	if err != nil {
		return fmt.Errorf("invalid compose template: %w", err)
	}

	if opts.DryRun {
		fmt.Printf("✨ [Dry Run] Rendered docker-compose.yml for %s:\n\n", app.Name)
		if err := tmpl.Execute(os.Stdout, ctx); err != nil {
			return fmt.Errorf("failed to render compose file to stdout: %w", err)
		}
		fmt.Printf("\n✨ [Dry Run] Would run: docker compose -f %s up -d\n", filepath.Join(appDir, "docker-compose.yml"))
		return nil
	}

	// Create directories
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dataDir, err)
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
