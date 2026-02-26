package cmd

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"gopkg.in/yaml.v3"
)

func runInit() error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  🏠 HomeButler Setup")
	fmt.Println("  ───────────────────")
	fmt.Println("  💡 Press Enter to accept [default] values")
	fmt.Println()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	cfgPath := filepath.Join(home, ".config", "homebutler", "config.yaml")

	cfg := &config.Config{
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}

	addMode := false // true = keep existing servers, just add new ones

	// Check existing config
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("  📂 Config found: %s\n", cfgPath)
		fmt.Println()

		existingCfg, loadErr := config.Load(cfgPath)
		if loadErr == nil && len(existingCfg.Servers) > 0 {
			fmt.Println("  Current servers:")
			for _, s := range existingCfg.Servers {
				if s.Local {
					fmt.Printf("    • %s (local)\n", s.Name)
				} else {
					fmt.Printf("    • %s → %s@%s:%d\n", s.Name, s.SSHUser(), s.Host, s.SSHPort())
				}
			}
			fmt.Println()

			fmt.Println("  What would you like to do?")
			fmt.Println("    [1] Add servers to existing config (default)")
			fmt.Println("    [2] Start fresh (overwrite)")
			fmt.Println("    [3] Cancel")
			choice := promptDefault(scanner, "  Choice", "1")

			switch choice {
			case "2":
				fmt.Println()
				fmt.Println("  ⚠️  This will DELETE all existing servers:")
				for _, s := range existingCfg.Servers {
					if s.Local {
						fmt.Printf("    • %s (local)\n", s.Name)
					} else {
						fmt.Printf("    • %s → %s@%s:%d\n", s.Name, s.SSHUser(), s.Host, s.SSHPort())
					}
				}
				fmt.Println()
				if !promptYN(scanner, "  Are you sure?", false) {
					fmt.Println("  Aborted.")
					return nil
				}
				// fresh start, cfg stays empty
			case "3":
				fmt.Println("  Aborted.")
				return nil
			default:
				// keep existing
				cfg = existingCfg
				addMode = true
			}
			fmt.Println()
		}
	}

	// Step 1: Local machine (skip if add mode already has one)
	hasLocal := false
	for _, s := range cfg.Servers {
		if s.Local {
			hasLocal = true
			break
		}
	}

	if !hasLocal {
		fmt.Println("  📍 Step 1: Local Machine")
		fmt.Println()

		localName := detectHostname()
		localIP := detectLocalIP()
		if localName != "" || localIP != "" {
			desc := localName
			if localIP != "" {
				desc += " (" + localIP + ")"
			}
			fmt.Printf("  Detected: %s\n", desc)

			name := promptDefault(scanner, "  Name for this machine", shortHostname(localName))
			cfg.Servers = append(cfg.Servers, config.ServerConfig{
				Name:  name,
				Host:  localIP,
				Local: true,
			})
			fmt.Printf("  ✅ Added local: %s\n", name)
		} else {
			fmt.Println("  Could not detect local machine.")
			if promptYN(scanner, "  Add it manually?", true) {
				name := promptRequiredInput(scanner, "  Name: ")
				cfg.Servers = append(cfg.Servers, config.ServerConfig{
					Name:  name,
					Host:  "127.0.0.1",
					Local: true,
				})
				fmt.Printf("  ✅ Added local: %s\n", name)
			}
		}
	} else if addMode {
		fmt.Println("  📍 Local machine already configured, skipping.")
	}

	// Step 2: Remote servers
	fmt.Println()
	fmt.Println("  🌐 Step 2: Remote Servers")
	if addMode && len(cfg.Servers) > 0 {
		fmt.Println()
		fmt.Println("  Existing servers:")
		for _, s := range cfg.Servers {
			if s.Local {
				fmt.Printf("    • %s (local)\n", s.Name)
			} else {
				fmt.Printf("    • %s → %s@%s:%d\n", s.Name, s.SSHUser(), s.Host, s.SSHPort())
			}
		}
	}
	fmt.Println()

	for promptYN(scanner, "  Add a remote server?", true) {
		fmt.Println()

		server, err := promptRemoteServer(scanner, home)
		if err != nil {
			return err
		}

		cfg.Servers = append(cfg.Servers, *server)
		fmt.Printf("  ✅ Added remote: %s (%s@%s)\n", server.Name, server.SSHUser(), server.Host)

		// Connection test
		fmt.Print("  🔌 Testing connection... ")
		if testSSH(server) {
			fmt.Println("connected!")
		} else {
			fmt.Println("failed (check settings later)")
		}
		fmt.Println()
	}

	// Step 3: Summary
	fmt.Println()
	fmt.Println("  📋 Summary")
	fmt.Println("  ──────────")
	for _, s := range cfg.Servers {
		if s.Local {
			fmt.Printf("  • %s (local)\n", s.Name)
		} else {
			fmt.Printf("  • %s → %s@%s:%d\n", s.Name, s.SSHUser(), s.Host, s.SSHPort())
		}
	}
	fmt.Printf("  • Alerts: CPU %g%% / Memory %g%% / Disk %g%%\n",
		cfg.Alerts.CPU, cfg.Alerts.Memory, cfg.Alerts.Disk)
	fmt.Println()

	if !promptYN(scanner, "  Save config?", true) {
		fmt.Println("  Aborted.")
		return nil
	}

	// Save
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Println()
	fmt.Printf("  ✨ Config saved to %s\n", cfgPath)
	fmt.Println()
	fmt.Println("  Try it out:")
	fmt.Println("    homebutler status")
	fmt.Println("    homebutler tui")
	fmt.Println()
	return nil
}

func promptRemoteServer(scanner *bufio.Scanner, home string) (*config.ServerConfig, error) {
	server := &config.ServerConfig{}

	// Name + Host (required)
	server.Name = promptRequiredInput(scanner, "  Name: ")
	server.Host = promptRequiredInput(scanner, "  Host/IP: ")

	// User — default to current user
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "root"
	}
	server.User = promptDefault(scanner, "  SSH user", currentUser)

	// Port
	portStr := promptDefault(scanner, "  SSH port", "22")
	if port, err := strconv.Atoi(portStr); err == nil && port != 22 {
		server.Port = port
	}

	// Auth method
	keyFile := detectSSHKey(home)
	defaultAuth := "key"
	if keyFile == "" {
		defaultAuth = "password"
	}
	authChoice := promptDefault(scanner, "  Auth method (key/password)", defaultAuth)

	if strings.ToLower(authChoice) == "password" {
		server.AuthMode = "password"
		server.Password = promptRequiredInput(scanner, "  Password: ")
	} else {
		defaultKey := shortPath(keyFile, home)
		if defaultKey == "" {
			defaultKey = "~/.ssh/id_rsa"
		}
		server.KeyFile = promptDefault(scanner, "  Key file", defaultKey)
	}

	return server, nil
}

// promptDefault shows a prompt with a default value in brackets.
// Empty input accepts the default.
func promptDefault(scanner *bufio.Scanner, prompt, def string) string {
	fmt.Printf("%s [%s]: ", prompt, def)
	if !scanner.Scan() {
		return def
	}
	val := strings.TrimSpace(scanner.Text())
	if val == "" {
		return def
	}
	return val
}

// promptRequiredInput loops until non-empty input is given.
func promptRequiredInput(scanner *bufio.Scanner, prompt string) string {
	for {
		fmt.Print(prompt)
		if !scanner.Scan() {
			continue
		}
		val := strings.TrimSpace(scanner.Text())
		if val != "" {
			return val
		}
	}
}

// promptYN asks a yes/no question. Default determines what Enter does.
func promptYN(scanner *bufio.Scanner, prompt string, def bool) bool {
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	fmt.Printf("%s [%s]: ", prompt, hint)
	if !scanner.Scan() {
		return def
	}
	val := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if val == "" {
		return def
	}
	return val == "y" || val == "yes"
}

// detectHostname returns the system hostname.
func detectHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// shortHostname strips domain parts: "my-mac.local" → "my-mac"
func shortHostname(name string) string {
	if i := strings.Index(name, "."); i > 0 {
		return name[:i]
	}
	if name == "" {
		return "localhost"
	}
	return name
}

// detectLocalIP finds the primary LAN IP (non-loopback).
func detectLocalIP() string {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 2*time.Second)
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}

// detectSSHKey finds the first existing SSH private key.
func detectSSHKey(home string) string {
	candidates := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}
	for _, k := range candidates {
		if _, err := os.Stat(k); err == nil {
			return k
		}
	}
	return ""
}

// shortPath replaces home dir prefix with ~
func shortPath(path, home string) string {
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// testSSH tries a quick SSH connection to verify settings.
func testSSH(server *config.ServerConfig) bool {
	port := server.SSHPort()
	addr := net.JoinHostPort(server.Host, strconv.Itoa(port))

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
