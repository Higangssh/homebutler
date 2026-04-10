package alerts

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

var (
	promptTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	promptOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	promptDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	promptAccent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
)

// InitResult holds the collected user choices.
type InitResult struct {
	CPUThreshold    float64
	MemoryThreshold float64
	DiskThreshold   float64
	Containers      []string
	ContainerAction string // "restart" or "notify"
	WebhookURL      string
}

// ContainerLister abstracts docker.List for testing.
type ContainerLister func() ([]docker.Container, error)

// RunInitPrompt drives the interactive alerts init wizard.
// It reads from r and writes to w so it can be tested without a real terminal.
func RunInitPrompt(r io.Reader, w io.Writer, listContainers ContainerLister) (*InitResult, error) {
	scanner := bufio.NewScanner(r)
	result := &InitResult{}

	fmt.Fprintln(w)
	fmt.Fprintln(w, promptTitle.Render("🛡️  Self-Healing Setup"))
	fmt.Fprintln(w)

	// --- Thresholds ---
	var err error
	result.CPUThreshold, err = askThreshold(scanner, w, "CPU", 90)
	if err != nil {
		return nil, err
	}
	result.MemoryThreshold, err = askThreshold(scanner, w, "Memory", 85)
	if err != nil {
		return nil, err
	}
	result.DiskThreshold, err = askThreshold(scanner, w, "Disk", 85)
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(w)

	// --- Docker containers ---
	containers, dockerErr := listContainers()
	if dockerErr != nil {
		fmt.Fprintln(w, promptDim.Render("  Docker not available — skipping container setup."))
		fmt.Fprintln(w)
	} else if len(containers) == 0 {
		fmt.Fprintln(w, promptDim.Render("  No containers found — skipping container setup."))
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, promptTitle.Render("📦 Detected containers:"))
		for i, c := range containers {
			state := promptOK.Render("running")
			if c.State != "running" {
				state = promptDim.Render(c.State)
			}
			fmt.Fprintf(w, "  [%d] %s (%s)\n", i+1, promptAccent.Render(c.Name), state)
		}

		fmt.Fprintf(w, "\n%s\n", promptDim.Render("? Select containers to watch:"))
		fmt.Fprintln(w, promptDim.Render("  Enter numbers separated by commas (e.g. 1,2), 'all', or press Enter to skip"))
		fmt.Fprintf(w, "%s ", promptDim.Render("  >"))
		selection := readLine(scanner)
		result.Containers = parseContainerSelection(selection, containers)

		if len(result.Containers) > 0 {
			// Confirm selection
			fmt.Fprintf(w, "\n  → Watching: %s\n", promptAccent.Render(strings.Join(result.Containers, ", ")))
			fmt.Fprintf(w, "%s ", promptDim.Render("  Correct? [Y/n]:"))
			confirm := strings.TrimSpace(strings.ToLower(readLine(scanner)))
			if confirm == "n" || confirm == "no" {
				result.Containers = nil
				fmt.Fprintln(w, promptDim.Render("  Skipped container monitoring."))
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, promptDim.Render("? When a container goes down:"))
			fmt.Fprintln(w, "  [1] Restart automatically")
			fmt.Fprintln(w, "  [2] Notify only")
			fmt.Fprintf(w, "%s ", promptDim.Render("? Choose (default: 1):"))
			choice := strings.TrimSpace(readLine(scanner))
			if choice == "2" {
				result.ContainerAction = "notify"
			} else {
				result.ContainerAction = "restart"
			}
		}
		fmt.Fprintln(w)
	}

	// --- Webhook URL ---
	fmt.Fprintf(w, "%s ", promptDim.Render("? Webhook URL (press Enter to skip):"))
	result.WebhookURL = strings.TrimSpace(readLine(scanner))

	return result, nil
}

// BuildYAML converts an InitResult into a user-friendly config.yaml snippet.
func BuildYAML(res *InitResult) (string, error) {
	type webhookConfig struct {
		URL string `yaml:"url,omitempty"`
	}
	type notifyConfig struct {
		Webhook *webhookConfig `yaml:"webhook,omitempty"`
	}
	type watchConfig struct {
		Enabled  bool   `yaml:"enabled"`
		NotifyOn string `yaml:"notify_on"`
		Cooldown string `yaml:"cooldown,omitempty"`
	}
	type alertsSection struct {
		CPU    float64 `yaml:"cpu"`
		Memory float64 `yaml:"memory"`
		Disk   float64 `yaml:"disk"`
		Rules  []Rule  `yaml:"rules,omitempty"`
	}
	type userConfig struct {
		Notify notifyConfig  `yaml:"notify,omitempty"`
		Watch  watchConfig   `yaml:"watch"`
		Alerts alertsSection `yaml:"alerts"`
	}

	cfg := userConfig{
		Watch: watchConfig{
			Enabled:  res.WebhookURL != "",
			NotifyOn: "flapping",
			Cooldown: "5m",
		},
		Alerts: alertsSection{
			CPU:    res.CPUThreshold,
			Memory: res.MemoryThreshold,
			Disk:   res.DiskThreshold,
		},
	}

	if res.WebhookURL != "" {
		cfg.Notify.Webhook = &webhookConfig{URL: res.WebhookURL}
	}

	cfg.Alerts.Rules = append(cfg.Alerts.Rules,
		Rule{Name: "cpu-spike", Metric: "cpu", Threshold: res.CPUThreshold, Action: "notify"},
		Rule{Name: "memory-high", Metric: "memory", Threshold: res.MemoryThreshold, Action: "notify"},
		Rule{Name: "disk-full", Metric: "disk", Threshold: res.DiskThreshold, Action: "notify"},
	)

	if len(res.Containers) > 0 {
		cfg.Alerts.Rules = append(cfg.Alerts.Rules, Rule{
			Name:     "container-down",
			Metric:   "container",
			Watch:    res.Containers,
			Action:   res.ContainerAction,
			Cooldown: "5m",
		})
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}
	return string(data), nil
}

func askThreshold(scanner *bufio.Scanner, w io.Writer, label string, defaultVal int) (float64, error) {
	fmt.Fprintf(w, "%s ", promptDim.Render(fmt.Sprintf("? %s alert threshold (default: %d%%):", label, defaultVal)))
	input := strings.TrimSpace(readLine(scanner))
	if input == "" {
		return float64(defaultVal), nil
	}
	val, err := strconv.ParseFloat(input, 64)
	if err != nil || val <= 0 || val > 100 {
		return 0, fmt.Errorf("invalid threshold %q: must be a number between 1 and 100", input)
	}
	return val, nil
}

func readLine(scanner *bufio.Scanner) string {
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

func parseContainerSelection(input string, containers []docker.Container) []string {
	input = strings.TrimSpace(input)
	if input == "" || strings.EqualFold(input, "all") {
		names := make([]string, len(containers))
		for i, c := range containers {
			names[i] = c.Name
		}
		return names
	}

	var result []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx, err := strconv.Atoi(part)
		if err == nil && idx >= 1 && idx <= len(containers) {
			name := containers[idx-1].Name
			if !seen[name] {
				result = append(result, name)
				seen[name] = true
			}
		}
	}
	return result
}
