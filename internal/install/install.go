package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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

func fixPermissionsHint(path string) string {
	return "sudo chown -R $(id -u):$(id -g) " + util.ShellQuote(path)
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
		if util.IsPermissionError(err) {
			return util.NewHintError("cannot create registry dir "+BaseDir(), err, fixPermissionsHint(filepath.Dir(BaseDir())))
		}
		return err
	}
	if err := os.WriteFile(registryFile(), data, 0644); err != nil {
		if util.IsPermissionError(err) {
			return util.NewHintError("cannot write install registry "+registryFile(), err, fixPermissionsHint(registryFile()))
		}
		return err
	}
	return nil
}

// removeInstalled removes an app from the registry.
func removeInstalled(appName string) error {
	all := loadInstalled()
	delete(all, appName)

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(registryFile(), data, 0644); err != nil {
		if util.IsPermissionError(err) {
			return util.NewHintError("cannot update install registry "+registryFile(), err, fixPermissionsHint(registryFile()))
		}
		return err
	}
	return nil
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
// App is a catalogue entry, and install_list and GET /api/install answer with
// it. It had no tags, so it went out as Name, DefaultPort and the rest, the only
// capitalised keys in any answer, with the whole compose template alongside;
// 1.0 would have frozen both.
type App struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	ComposeFile   string `json:"-"`              // go template for docker-compose.yml
	DefaultPort   string `json:"default_port"`   // default host port
	ContainerPort string `json:"container_port"` // container port (fixed)
	DataPath      string `json:"data_path"`      // container data path
	// StateDirs are the app's own state, kept beside data/ in the app
	// directory and bound into the container by the compose file. Install
	// creates them before compose up: Docker would create a missing bind
	// source as root, and an image that runs as a non-root user then cannot
	// write to it.
	StateDirs []StateDir `json:"-"`
	// ImageUser is the uid:gid the image runs as by itself. Install hands
	// the state dirs to it when it runs as root and leaves user: out.
	ImageUser string `json:"-"`
}

// StateDir is one directory of an app's state: Name under the app
// directory, bound to Path in the container.
type StateDir struct {
	Name string
	Path string
}

// ContainerName is the name the app's container runs under, read from its
// compose file. It is usually the app's own name and not always: pi-hole's
// container is pihole, and a hint that names the app where Docker wants the
// container sends the reader to "No such container".
func (a App) ContainerName() string {
	for _, line := range strings.Split(a.ComposeFile, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "container_name:"); ok {
			return strings.TrimSpace(name)
		}
	}
	return a.Name
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
	AppDir       string
	DataDir      string
	UID          int
	GID          int
	User         string // user: for an app with state dirs; see containerUser
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
	"plex": {
		Name:          "plex",
		Description:   "Plex Media Server - organize and stream your media",
		DefaultPort:   "32400",
		ContainerPort: "32400",
		DataPath:      "/config",
		ComposeFile: `services:
  plex:
    image: plexinc/pms-docker:latest
    container_name: plex
    restart: unless-stopped
    ports:
      - "{{.Port}}:32400"
    volumes:
      - "{{.DataDir}}/config:/config"
      - "{{.DataDir}}/transcode:/transcode"{{if .MediaDir}}
      - "{{.MediaDir}}:/data:ro"{{end}}
    environment:
      - PUID={{.UID}}
      - PGID={{.GID}}
      - VERSION=docker
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
		Description:   "Web-based file manager. Its upstream project is archived and gets no further releases or security fixes",
		DefaultPort:   "8081",
		ContainerPort: "80",
		DataPath:      "/srv",
		// The image declares /config and /database as volumes as well as
		// /srv. Left unmapped, each new container got fresh anonymous
		// volumes, and filebrowser.db did not survive an uninstall (#327).
		// They sit beside data/, not in it, because data/ is /srv and
		// everything under /srv can be browsed and downloaded. The image
		// runs as uid 1000; containerUser decides who owns the directories
		// Install creates and who the container runs as.
		StateDirs: []StateDir{{Name: "config", Path: "/config"}, {Name: "database", Path: "/database"}},
		ImageUser: "1000:1000",
		ComposeFile: `services:
  filebrowser:
    image: filebrowser/filebrowser:latest
    container_name: filebrowser
    restart: unless-stopped{{if .User}}
    user: "{{.User}}"{{end}}
    ports:
      - "{{.Port}}:80"
    volumes:
      - "{{.DataDir}}:/srv"
      - "{{.AppDir}}/config:/config"
      - "{{.AppDir}}/database:/database"
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

// ValidateAppName checks that an app name is safe for use in file paths.
// It rejects names containing path separators or traversal sequences.
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("app name must not be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid app name %q: must not contain '/', '\\', or '..'", name)
	}
	// After joining, the result must still be under BaseDir.
	joined := filepath.Join(BaseDir(), name)
	rel, err := filepath.Rel(BaseDir(), joined)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("invalid app name %q: path escapes base directory", name)
	}
	return nil
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
	dockerCmd      = util.DockerCmd
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
	if _, err := dockerCmd("info"); err != nil {
		issues = append(issues, "docker daemon is not running.\n"+
			"    Try: sudo systemctl start docker   (Linux)\n"+
			"         colima start                   (macOS)")
		return issues
	}

	// Check docker compose is available
	if _, err := dockerCmd("compose", "version"); err != nil {
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
		dockerOut, _ := dockerCmd("ps", "--format", "{{.Names}} {{.Ports}}")
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
		stateOut, _ := dockerCmd("compose", "-f", composeFile, "ps", "--format", "{{.State}}")
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
	case "plex":
		return "Plex is starting. Initial setup is required via the web interface."
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

// portInUseBy checks if a port is in use (cross-platform).
func portInUseBy(port string) string {
	// Validate port is purely numeric to prevent shell injection.
	for _, c := range port {
		if c < '0' || c > '9' {
			return ""
		}
	}

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
// The Carryover says what it did with an earlier install's state, if anything.
func Install(app App, opts InstallOptions) (Carryover, error) {
	var carried Carryover
	port := app.DefaultPort
	if opts.Port != "" {
		port = opts.Port
	}

	if err := ValidatePort(port); err != nil {
		return carried, err
	}

	appDir := AppDir(app.Name)
	dataDir := filepath.Join(appDir, "data")
	user, chownTo := containerUser(app)

	// Render docker-compose.yml
	ctx := composeContext{
		Port:         port,
		AppDir:       appDir,
		DataDir:      dataDir,
		UID:          os.Getuid(),
		GID:          os.Getgid(),
		User:         user,
		MediaDir:     opts.MediaDir,
		DockerSocket: util.DockerSocket(),
	}

	tmpl, err := template.New("compose").Parse(app.ComposeFile)
	if err != nil {
		return carried, fmt.Errorf("invalid compose template: %w", err)
	}

	if opts.DryRun {
		fmt.Printf("✨ [Dry Run] Rendered docker-compose.yml for %s:\n\n", app.Name)
		if err := tmpl.Execute(os.Stdout, ctx); err != nil {
			return carried, fmt.Errorf("failed to render compose file to stdout: %w", err)
		}
		fmt.Printf("\n✨ [Dry Run] Would run: docker compose -f %s up -d\n", filepath.Join(appDir, "docker-compose.yml"))
		return carried, nil
	}

	// Create directories (only for real installs)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		if util.IsPermissionError(err) {
			return carried, util.NewHintError("cannot create directory "+dataDir, err, "sudo homebutler install "+app.Name)
		}
		return carried, fmt.Errorf("failed to create directory %s: %w", dataDir, err)
	}

	// Before the old compose file is replaced: it is what says where an
	// earlier install kept its state.
	carried, err = carryOverState(app, appDir)
	if err != nil {
		return carried, fmt.Errorf("did not replace the earlier %s install: %w", app.Name, err)
	}
	for _, d := range app.StateDirs {
		dir := filepath.Join(appDir, d.Name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			if util.IsPermissionError(err) {
				return carried, util.NewHintError("cannot create directory "+dir, err, "sudo homebutler install "+app.Name)
			}
			return carried, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		if chownTo != nil {
			if err := chownTree(dir, chownTo[0], chownTo[1]); err != nil {
				return carried, fmt.Errorf("failed to hand %s to the image's user: %w", dir, err)
			}
		}
	}

	composeFile := filepath.Join(appDir, "docker-compose.yml")
	f, err := os.Create(composeFile)
	if err != nil {
		if util.IsPermissionError(err) {
			return carried, util.NewHintError("cannot create "+composeFile, err, "sudo homebutler install "+app.Name)
		}
		return carried, fmt.Errorf("failed to create %s: %w", composeFile, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, ctx); err != nil {
		f.Close()
		os.Remove(composeFile)
		return carried, fmt.Errorf("failed to render compose file: %w", err)
	}

	// Run docker compose up
	_, err = dockerCmd("compose", "-f", composeFile, "up", "-d")
	if err != nil {
		return carried, fmt.Errorf("failed to start %s: %w", app.Name, err)
	}

	// Record install location
	return carried, saveInstalled(installedApp{
		Name: app.Name,
		Path: appDir,
		Port: port,
	})
}

// containerUser is the user an app with state dirs runs as, and the owner to
// give those dirs, if they need one. It is the installing user, so the
// container owns the directories Install creates, with two exceptions:
//
//   - Under rootless Docker or Podman, the container's root is the installing
//     user on the host, and any other uid is a subordinate one that cannot
//     write to them. So it runs as 0:0.
//   - When the installer is root (sudo), running the container as root would
//     give filebrowser more than its image asks for. It runs as the image's
//     own user, and the dirs are handed to that user instead.
//
// An app without state dirs keeps whatever its image or template says.
func containerUser(app App) (string, []int) {
	if len(app.StateDirs) == 0 {
		return "", nil
	}
	if out, err := dockerCmd("info", "--format", "{{.SecurityOptions}}"); err == nil && strings.Contains(out, "rootless") {
		return "0:0", nil
	}
	if os.Geteuid() == 0 {
		var uid, gid int
		if _, err := fmt.Sscanf(app.ImageUser, "%d:%d", &uid, &gid); err == nil {
			return "", []int{uid, gid}
		}
		return "", nil
	}
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), nil
}

// chownTree gives dir and everything under it to uid:gid.
func chownTree(dir string, uid, gid int) error {
	return filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	})
}

// Uninstall stops the app and removes its containers. The Carryover says what
// it did with an earlier install's state, if anything.
func Uninstall(appName string) (Carryover, error) {
	var carried Carryover
	if err := ValidateAppName(appName); err != nil {
		return carried, err
	}
	appDir := GetInstalledPath(appName)
	composeFile := filepath.Join(appDir, "docker-compose.yml")

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return carried, fmt.Errorf("%s is not installed", appName)
	}

	// down removes the container, and with it the only record of which
	// anonymous volumes held an older install's state, so copy that first.
	if app, ok := Registry[appName]; ok {
		var err error
		carried, err = carryOverState(app, appDir)
		if err != nil {
			return carried, fmt.Errorf("did not remove %s's container: %w", appName, err)
		}
	}

	// docker compose down
	if _, err := dockerCmd("compose", "-f", composeFile, "down"); err != nil {
		return carried, fmt.Errorf("failed to stop %s: %w", appName, err)
	}

	return carried, nil
}

// PurgeResult says which of an app's Docker volumes a purge removed and which
// are still there. A volume is only counted as removed when Docker answers
// that it does not exist.
type PurgeResult struct {
	Removed []string
	// Kept holds volumes Docker says still exist after `down -v`: external,
	// or in use by a container outside the app. Saying the data was deleted
	// while one of these remains would be the claim #324 was about.
	Kept []string
	// Unchecked holds volumes Docker could not be asked about afterwards,
	// because the daemon or permissions failed. They may or may not be gone.
	Unchecked []string
}

// Complete reports whether every volume the app used is confirmed gone.
func (r PurgeResult) Complete() bool {
	return len(r.Kept) == 0 && len(r.Unchecked) == 0
}

// Purge stops an app, removes its containers together with the Docker volumes
// they used, and deletes its directory. It returns what happened to each
// volume, checked after the fact, so a caller can say what "data" meant.
//
// The volumes matter because the app directory is not all of the data. An
// image can declare a VOLUME its compose file does not map, and Docker then
// gives each container an anonymous volume that `compose down` leaves behind.
// filebrowser keeps its database and settings in two of them, so purge said
// "Completely removed" and left both — on the soak machine and again on the
// demo host (#324). `down -v` removes those and any volume the compose file
// declares; none in the catalogue declares one, and a volume marked external
// is never removed by compose. Uninstall keeps running plain `down`, because
// keeping the data is the difference between the two.
func Purge(appName string) (PurgeResult, error) {
	if err := ValidateAppName(appName); err != nil {
		return PurgeResult{}, err
	}
	appDir := GetInstalledPath(appName)
	composeFile := filepath.Join(appDir, "docker-compose.yml")
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return PurgeResult{}, fmt.Errorf("%s is not installed", appName)
	}

	// Read before the containers go, since they are what names the volumes.
	// Best effort: a purge that cannot list them still removes them.
	var volumes []string
	if ids, err := dockerCmd("compose", "-f", composeFile, "ps", "-a", "-q"); err == nil && strings.TrimSpace(ids) != "" {
		args := append([]string{"inspect", "--format", volumeMountsFormat}, strings.Fields(ids)...)
		if out, err := dockerCmd(args...); err == nil {
			volumes = parseVolumeNames(out)
		}
	}

	// The anonymous volumes an earlier install's state was copied out of
	// (#327). No container mounts them any more, so the lookup above does
	// not find them, and they still hold that state.
	// Only what reads as an anonymous volume's ID is removed. The record is
	// a file anyone can edit, and a name in it could be someone's named
	// volume: that is reported, never removed.
	var carried, notAnonymous []string
	for _, v := range carriedVolumes(appDir) {
		if anonymousVolumeID.MatchString(v) {
			carried = append(carried, v)
		} else {
			notAnonymous = append(notAnonymous, v)
		}
	}

	if _, err := dockerCmd("compose", "-f", composeFile, "down", "-v"); err != nil {
		return PurgeResult{}, fmt.Errorf("failed to stop %s: %w", appName, err)
	}
	// Recorded only for volumes that were anonymous state mounts of a
	// catalogue template that declares no named volume, and rm refuses a
	// volume still in use, so these need no label check.
	for _, v := range carried {
		_, _ = dockerCmd("volume", "rm", v)
	}
	volumes = parseVolumeNames(strings.Join(append(volumes, carried...), "\n"))
	// `down -v` misses the anonymous volumes of a container that was ever
	// recreated: compose carries them over to the new container, and Docker
	// then does not count them as that container's own (#335). Remove the
	// ones this app's containers used that are anonymous and now unused.
	// Docker's own label marks them; a named, external or in-use volume is
	// never touched here.
	if len(volumes) > 0 {
		if out, err := dockerCmd("volume", "ls", "-q", "--filter", "dangling=true", "--filter", "label=com.docker.volume.anonymous"); err == nil {
			for _, v := range orphanedAnonymous(volumes, out) {
				_, _ = dockerCmd("volume", "rm", v)
			}
		}
	}
	if err := removeInstalled(appName); err != nil {
		return PurgeResult{}, err
	}
	// Try normal remove first
	err := os.RemoveAll(appDir)
	if err != nil {
		// Docker may create files as root — try passwordless sudo
		_, sudoErr := util.RunCmd("sudo", "-n", "rm", "-rf", appDir)
		if sudoErr != nil {
			return PurgeResult{}, fmt.Errorf("permission denied. Docker creates files as root.\n"+
				"    Run: sudo rm -rf %s", appDir)
		}
	}

	result := classifyVolumes(volumes, func(name string) (string, error) {
		return dockerCmd("volume", "inspect", name)
	})
	// Purge did not remove these, so none of them counts as removed: still
	// there is kept, and anything else is unchecked.
	for _, v := range notAnonymous {
		if slices.Contains(volumes, v) {
			continue
		}
		if _, err := dockerCmd("volume", "inspect", v); err == nil {
			result.Kept = append(result.Kept, v)
		} else {
			result.Unchecked = append(result.Unchecked, v)
		}
	}
	return result, nil
}

// anonymousVolumeID is what Docker names an anonymous volume.
var anonymousVolumeID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// orphanedAnonymous returns the volumes in listed that also appear in the
// output of `docker volume ls -q --filter dangling=true --filter
// label=com.docker.volume.anonymous`: this app's, anonymous, and used by no
// container.
func orphanedAnonymous(listed []string, danglingAnonymous string) []string {
	candidates := map[string]bool{}
	for _, name := range strings.Fields(danglingAnonymous) {
		candidates[name] = true
	}
	var out []string
	for _, v := range listed {
		if candidates[v] {
			out = append(out, v)
		}
	}
	return out
}

// classifyVolumes asks Docker about each volume after the purge. Only an
// answer of "no such volume" counts as removed: inspect also fails when the
// daemon is unreachable or permission is refused, and reading that as gone
// would report a deletion nobody checked.
func classifyVolumes(names []string, inspect func(string) (string, error)) PurgeResult {
	var r PurgeResult
	for _, v := range names {
		out, err := inspect(v)
		switch {
		case err == nil:
			r.Kept = append(r.Kept, v)
		case strings.Contains(strings.ToLower(out), "no such volume"):
			r.Removed = append(r.Removed, v)
		default:
			r.Unchecked = append(r.Unchecked, v)
		}
	}
	return r
}

// volumeMountsFormat prints one Docker volume name per line for each
// container inspected; bind mounts are left out.
const volumeMountsFormat = `{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}`

// parseVolumeNames reads volumeMountsFormat's output: one name per line,
// in order, each once.
func parseVolumeNames(out string) []string {
	seen := map[string]bool{}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// Outcome is how an install, uninstall or purge went, as the MCP tools and
// the dashboard's routes both report it. They are two call sites answering
// the same question, and they used to spell the answer separately.
type Outcome string

const (
	OutcomeInstalled   Outcome = "installed"
	OutcomeFailed      Outcome = "failed"
	OutcomeUninstalled Outcome = "uninstalled"
	OutcomePurged      Outcome = "purged"
)

// Outcomes is the whole vocabulary.
func Outcomes() []Outcome {
	return []Outcome{OutcomeInstalled, OutcomeFailed, OutcomeUninstalled, OutcomePurged}
}

// Status checks if the installed app is running.
// ErrNotInstalled is what Status answers for an app that is in the catalogue
// and has never been installed. That is the ordinary state of almost every
// entry, so a caller has to be able to tell it apart from a failure: the HTTP
// route said 500 for it until somebody looked.
var ErrNotInstalled = errors.New("not installed")

// Installed reports whether an app has been installed here, without asking
// Docker anything.
func Installed(appName string) bool {
	if err := ValidateAppName(appName); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(GetInstalledPath(appName), "docker-compose.yml"))
	return err == nil
}

func Status(appName string) (string, error) {
	if err := ValidateAppName(appName); err != nil {
		return "", err
	}
	appDir := GetInstalledPath(appName)
	composeFile := filepath.Join(appDir, "docker-compose.yml")

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return "", fmt.Errorf("%s is %w", appName, ErrNotInstalled)
	}

	out, err := dockerCmd("compose", "-f", composeFile, "ps",
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
