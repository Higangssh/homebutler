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

		fmt.Fprintf(w, "\n%s ", promptDim.Render("? Select containers to watch (comma-separated, or 'all'):"))
		selection := readLine(scanner)
		result.Containers = parseContainerSelection(selection, containers)

		if len(result.Containers) > 0 {
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

// BuildYAML converts an InitResult into an AlertsConfig YAML string.
func BuildYAML(res *InitResult) (string, error) {
	cfg := AlertsConfig{}

	cfg.Rules = append(cfg.Rules, Rule{
		Name:      "cpu-spike",
		Metric:    "cpu",
		Threshold: res.CPUThreshold,
		Action:    "notify",
		Notify:    "webhook",
	})
	cfg.Rules = append(cfg.Rules, Rule{
		Name:      "memory-high",
		Metric:    "memory",
		Threshold: res.MemoryThreshold,
		Action:    "notify",
		Notify:    "webhook",
	})
	cfg.Rules = append(cfg.Rules, Rule{
		Name:      "disk-full",
		Metric:    "disk",
		Threshold: res.DiskThreshold,
		Action:    "notify",
		Notify:    "webhook",
	})

	if len(res.Containers) > 0 {
		cfg.Rules = append(cfg.Rules, Rule{
			Name:     "container-down",
			Metric:   "container",
			Watch:    res.Containers,
			Action:   res.ContainerAction,
			Notify:   "webhook",
			Cooldown: "5m",
		})
	}

	cfg.Webhook = WebhookConfig{URL: res.WebhookURL}

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
