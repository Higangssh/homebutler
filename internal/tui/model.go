package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/system"
)

const refreshInterval = 2 * time.Second

// tickMsg triggers a data refresh cycle.
type tickMsg time.Time

// dataMsg delivers fetched server data back to the model.
type dataMsg struct {
	index int
	data  ServerData
}

// dockerMsg delivers docker data separately (non-blocking).
type dockerMsg struct {
	index      int
	containers []docker.Container
	status     string
}

// serverTab represents one monitored server in the dashboard.
type serverTab struct {
	config     *config.ServerConfig
	data       ServerData
	cpuHistory []float64
	memHistory []float64
}

// Model is the Bubble Tea model for the watch dashboard.
type Model struct {
	servers   []serverTab
	activeTab int
	width     int
	height    int
	cfg       *config.Config
	quitting  bool
}

// panelWidths holds all pre-computed widths for consistent layout.
type panelWidths struct {
	leftW    int // Width() param for left panelStyle (outer - border)
	rightW   int // Width() param for right panelStyle
	footerW  int // Width() param for footer panelStyle
	barWidth int // progress bar character width
}

// calcWidths computes all panel widths from terminal width.
// panelStyle has Border(Rounded)=2 horizontal, Padding(1,2)=4 horizontal.
// Width(w) sets the area including padding but excluding borders,
// so outer = w + 2, and content text area = w - 4.
func (m Model) calcWidths() panelWidths {
	const hBorder = 2
	const hPad = 4

	leftOuter := m.width * 2 / 5
	if leftOuter < 30 {
		leftOuter = 30
	}
	rightOuter := m.width - leftOuter
	if rightOuter < 30 {
		rightOuter = 30
	}

	leftW := leftOuter - hBorder
	rightW := rightOuter - hBorder
	leftInner := leftW - hPad

	// footer spans both panels exactly: outer = leftOuter + rightOuter
	footerW := leftOuter + rightOuter - hBorder

	barWidth := leftInner - 14
	if barWidth < 5 {
		barWidth = 5
	}

	return panelWidths{
		leftW:    leftW,
		rightW:   rightW,
		footerW:  footerW,
		barWidth: barWidth,
	}
}

// NewModel creates a dashboard model for the given servers.
// If serverNames is empty, all configured servers are used.
func NewModel(cfg *config.Config, serverNames []string) Model {
	var tabs []serverTab

	// Initialize with empty StatusInfo so the UI renders immediately
	// (CPU/Mem show 0% until first real fetch completes)
	emptyStatus := &system.StatusInfo{}

	if len(serverNames) == 0 {
		for i := range cfg.Servers {
			tabs = append(tabs, serverTab{
				config: &cfg.Servers[i],
				data:   ServerData{Name: cfg.Servers[i].Name, Status: emptyStatus},
			})
		}
	} else {
		for _, name := range serverNames {
			if srv := cfg.FindServer(name); srv != nil {
				tabs = append(tabs, serverTab{
					config: srv,
					data:   ServerData{Name: srv.Name, Status: emptyStatus},
				})
			}
		}
	}

	// Fallback: monitor local machine
	if len(tabs) == 0 {
		tabs = append(tabs, serverTab{
			config: &config.ServerConfig{Name: "local", Local: true},
			data:   ServerData{Name: "local"},
		})
	}

	return Model{
		servers: tabs,
		cfg:     cfg,
	}
}

// Init starts the initial data fetch and tick timer.
func (m Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.servers)*2+1)
	for i := range m.servers {
		idx := i
		srv := m.servers[i]
		// System data (fast)
		cmds = append(cmds, func() tea.Msg {
			data := fetchServer(srv.config, &m.cfg.Alerts)
			return dataMsg{index: idx, data: data}
		})
		// Docker data separately (may be slow on local)
		if srv.config.Local {
			cmds = append(cmds, func() tea.Msg {
				containers, status := fetchDocker()
				return dockerMsg{index: idx, containers: containers, status: status}
			})
		}
	}
	cmds = append(cmds, tickCmd())
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages (keys, window resize, data, tick).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			if len(m.servers) > 1 {
				m.activeTab = (m.activeTab + 1) % len(m.servers)
			}
		case "shift+tab":
			if len(m.servers) > 1 {
				m.activeTab = (m.activeTab - 1 + len(m.servers)) % len(m.servers)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		cmds := make([]tea.Cmd, 0, len(m.servers)*2+1)
		for i := range m.servers {
			idx := i
			srv := m.servers[i]
			cmds = append(cmds, func() tea.Msg {
				return dataMsg{index: idx, data: fetchServer(srv.config, &m.cfg.Alerts)}
			})
			if srv.config.Local {
				cmds = append(cmds, func() tea.Msg {
					containers, status := fetchDocker()
					return dockerMsg{index: idx, containers: containers, status: status}
				})
			}
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case dataMsg:
		if msg.index >= 0 && msg.index < len(m.servers) {
			// Preserve docker data (fetched separately)
			prevDocker := m.servers[msg.index].data.DockerStatus
			prevContainers := m.servers[msg.index].data.Containers
			m.servers[msg.index].data = msg.data
			if msg.data.DockerStatus == "" && prevDocker != "" {
				m.servers[msg.index].data.DockerStatus = prevDocker
				m.servers[msg.index].data.Containers = prevContainers
			}
			// Track CPU/Memory history for sparklines
			if msg.data.Status != nil {
				m.servers[msg.index].cpuHistory = appendHistory(
					m.servers[msg.index].cpuHistory, msg.data.Status.CPU.UsagePercent)
				m.servers[msg.index].memHistory = appendHistory(
					m.servers[msg.index].memHistory, msg.data.Status.Memory.Percent)
			}
		}

	case dockerMsg:
		if msg.index >= 0 && msg.index < len(m.servers) {
			m.servers[msg.index].data.DockerStatus = msg.status
			m.servers[msg.index].data.Containers = msg.containers
		}
	}

	return m, nil
}

// View renders the full dashboard.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "  Loading dashboard..."
	}

	w := m.calcWidths()
	var b strings.Builder

	// Tabs
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	// Main content
	if m.activeTab < len(m.servers) {
		tab := m.servers[m.activeTab]
		if tab.data.Error != nil {
			errPanel := panelStyle.Width(w.footerW).Render(
				criticalStyle.Render(fmt.Sprintf("  Error: %v", tab.data.Error)))
			b.WriteString(errPanel)
			b.WriteString("\n")
		} else {
			b.WriteString(m.renderContent(tab.data, w))
		}
	}

	// Footer
	b.WriteString(m.renderFooter(w))

	return b.String()
}

// renderTabs draws the server tab bar.
func (m Model) renderTabs() string {
	var tabs []string
	for i, srv := range m.servers {
		name := srv.data.Name
		if name == "" {
			name = srv.config.Name
		}
		label := fmt.Sprintf(" [%d] %s ", i+1, name)
		if i == m.activeTab {
			tabs = append(tabs, activeTabStyle.Render(label))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(label))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	serverCount := dimStyle.Render(fmt.Sprintf("  (%d available · Tab to switch)", len(m.servers)))
	return tabBar + serverCount
}

// renderContent draws left (system) and right (docker+processes) panels side by side.
func (m Model) renderContent(data ServerData, w panelWidths) string {
	left := m.renderSystemPanel(data, w)
	right := m.renderRightPanel(data, w)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n"
}

// renderSystemPanel draws CPU/memory/disk bars, history sparklines, and system info.
func (m Model) renderSystemPanel(data ServerData, w panelWidths) string {
	var lines []string
	serverName := data.Name
	if serverName == "" {
		serverName = "System"
	}
	lines = append(lines, titleStyle.Render("⚡ "+serverName))
	lines = append(lines, "")

	if data.Status != nil {
		s := data.Status
		bw := w.barWidth

		// Metrics: progress bars with no blank lines between them
		lines = append(lines, fmt.Sprintf("  CPU  %s %5.1f%%",
			progressBar(s.CPU.UsagePercent, bw), s.CPU.UsagePercent))
		lines = append(lines, fmt.Sprintf("  Mem  %s %5.1f%%",
			progressBar(s.Memory.Percent, bw), s.Memory.Percent))
		for _, d := range s.Disks {
			label := truncate(d.Mount, 4)
			lines = append(lines, fmt.Sprintf("  %-4s %s %5.1f%%",
				label, progressBar(d.Percent, bw), d.Percent))
		}

		// History section with dim divider
		lines = append(lines, "")
		tab := m.servers[m.activeTab]

		dividerLabel := "── History (2min) "
		leftInner := w.barWidth + 14
		dividerFill := leftInner - 2 - lipgloss.Width(dividerLabel)
		if dividerFill < 0 {
			dividerFill = 0
		}
		lines = append(lines, dimStyle.Render("  "+dividerLabel+strings.Repeat("─", dividerFill)))

		// CPU sparkline
		if len(tab.cpuHistory) > 0 {
			rendered := sparklineColor(tab.cpuHistory).Render(sparkline(tab.cpuHistory, bw))
			lines = append(lines, "  CPU "+lipgloss.NewStyle().Width(bw).Render(rendered))
		} else {
			lines = append(lines, "  CPU "+lipgloss.NewStyle().Width(bw).Render(
				dimStyle.Render(strings.Repeat("▁", bw))))
		}

		// Mem sparkline
		if len(tab.memHistory) > 0 {
			rendered := sparklineColor(tab.memHistory).Render(sparkline(tab.memHistory, bw))
			lines = append(lines, "  Mem "+lipgloss.NewStyle().Width(bw).Render(rendered))
		} else {
			lines = append(lines, "  Mem "+lipgloss.NewStyle().Width(bw).Render(
				dimStyle.Render(strings.Repeat("▁", bw))))
		}

		// Info section
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Uptime:  %s", s.Uptime))
		lines = append(lines, fmt.Sprintf("  OS:      %s/%s", s.OS, s.Arch))
		lines = append(lines, fmt.Sprintf("  Cores:   %d", s.CPU.Cores))
		lines = append(lines, fmt.Sprintf("  Memory:  %.1f / %.1f GB", s.Memory.UsedGB, s.Memory.TotalGB))
	} else {
		lines = append(lines, dimStyle.Render("  Waiting for data..."))
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Width(w.leftW).Render(content)
}

// renderRightPanel draws Docker containers and top processes in a single panel.
func (m Model) renderRightPanel(data ServerData, w panelWidths) string {
	var lines []string

	// Docker section
	lines = append(lines, titleStyle.Render("Docker Containers"))
	lines = append(lines, "")

	switch data.DockerStatus {
	case "not_installed":
		lines = append(lines, dimStyle.Render("  Docker not installed"))
	case "unavailable":
		lines = append(lines, warningStyle.Render("  Docker unavailable (daemon not running?)"))
	case "ok":
		if len(data.Containers) == 0 {
			lines = append(lines, dimStyle.Render("  No containers"))
		} else {
			header := fmt.Sprintf("  %-18s %-10s %-10s %s", "NAME", "STATE", "IMAGE", "STATUS")
			lines = append(lines, headerStyle.Render(header))

			maxContainers := 8
			shown := data.Containers
			if len(shown) > maxContainers {
				shown = shown[:maxContainers]
			}
			for _, c := range shown {
				var stateStr string
				switch c.State {
				case "running":
					stateStr = okStyle.Render(fmt.Sprintf("%-10s", c.State))
				case "exited":
					stateStr = criticalStyle.Render(fmt.Sprintf("%-10s", c.State))
				default:
					stateStr = warningStyle.Render(fmt.Sprintf("%-10s", c.State))
				}
				line := fmt.Sprintf("  %-18s %s %-10s %s",
					truncate(c.Name, 18),
					stateStr,
					truncate(c.Image, 10),
					truncate(c.Status, 20))
				lines = append(lines, line)
			}
			if len(data.Containers) > maxContainers {
				lines = append(lines, dimStyle.Render(fmt.Sprintf(
					"  ... and %d more", len(data.Containers)-maxContainers)))
			}
		}
	default:
		lines = append(lines, dimStyle.Render("  Waiting for data..."))
	}

	// Two blank lines separating Docker and Processes (same panel)
	lines = append(lines, "")
	lines = append(lines, "")

	// Processes section
	lines = append(lines, titleStyle.Render("Top Processes (CPU)"))
	lines = append(lines, "")

	if len(data.Processes) == 0 {
		lines = append(lines, dimStyle.Render("  No process data"))
	} else {
		header := fmt.Sprintf("  %6s  %6s  %6s  %s", "PID", "CPU%", "MEM%", "NAME")
		lines = append(lines, headerStyle.Render(header))
		for _, p := range data.Processes {
			lines = append(lines, fmt.Sprintf("  %6d  %5.1f%%  %5.1f%%  %s",
				p.PID, p.CPU, p.Mem, truncate(p.Name, 30)))
		}
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Width(w.rightW).Render(content)
}

// renderFooter draws alerts and keybinding hints.
func (m Model) renderFooter(w panelWidths) string {
	var parts []string

	// Alerts line
	if m.activeTab < len(m.servers) {
		data := m.servers[m.activeTab].data
		if data.Alerts != nil {
			var alertParts []string
			a := data.Alerts
			alertParts = append(alertParts,
				alertStyle(a.CPU.Status).Render(fmt.Sprintf("CPU: %.0f%%", a.CPU.Current)))
			alertParts = append(alertParts,
				alertStyle(a.Memory.Status).Render(fmt.Sprintf("Mem: %.0f%%", a.Memory.Current)))
			for _, d := range a.Disks {
				alertParts = append(alertParts,
					alertStyle(d.Status).Render(fmt.Sprintf("Disk %s: %.0f%%", d.Mount, d.Current)))
			}
			parts = append(parts, "  Alerts: "+strings.Join(alertParts, "  "))
		}
	}

	// Keybinding hints
	var keys []string
	if len(m.servers) > 1 {
		keys = append(keys, headerStyle.Render("Tab/Shift+Tab")+" switch server")
	}
	keys = append(keys, headerStyle.Render("q")+" quit")
	keys = append(keys, dimStyle.Render(fmt.Sprintf("⟳ %ds", int(refreshInterval.Seconds()))))
	parts = append(parts, "  "+strings.Join(keys, "  │  "))

	footer := strings.Join(parts, "\n")
	return panelStyle.Width(w.footerW).Render(footer)
}

// Run starts the TUI dashboard.
func Run(cfg *config.Config, serverNames []string) error {
	m := NewModel(cfg, serverNames)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
