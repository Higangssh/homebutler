package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/system"
)

func testConfig() *config.Config {
	return &config.Config{
		Servers: []config.ServerConfig{
			{Name: "rpi5", Host: "192.168.1.10", Local: true},
			{Name: "nas", Host: "192.168.1.20"},
		},
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
	}
}

func TestNewModel_AllServers(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)
	if len(m.servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(m.servers))
	}
	if m.servers[0].config.Name != "rpi5" {
		t.Errorf("expected first server rpi5, got %s", m.servers[0].config.Name)
	}
	if m.servers[1].config.Name != "nas" {
		t.Errorf("expected second server nas, got %s", m.servers[1].config.Name)
	}
	if m.activeTab != 0 {
		t.Errorf("expected activeTab 0, got %d", m.activeTab)
	}
}

func TestNewModel_SpecificServer(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, []string{"nas"})
	if len(m.servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(m.servers))
	}
	if m.servers[0].config.Name != "nas" {
		t.Errorf("expected server nas, got %s", m.servers[0].config.Name)
	}
}

func TestNewModel_NoServers_FallbackLocal(t *testing.T) {
	cfg := &config.Config{Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90}}
	m := NewModel(cfg, nil)
	if len(m.servers) != 1 {
		t.Fatalf("expected 1 fallback server, got %d", len(m.servers))
	}
	if m.servers[0].config.Name != "local" {
		t.Errorf("expected local fallback, got %s", m.servers[0].config.Name)
	}
	if !m.servers[0].config.Local {
		t.Error("expected fallback server to be local")
	}
}

func TestNewModel_UnknownServer_FallbackLocal(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, []string{"nonexistent"})
	if len(m.servers) != 1 {
		t.Fatalf("expected 1 fallback server, got %d", len(m.servers))
	}
	if m.servers[0].config.Name != "local" {
		t.Errorf("expected local fallback, got %s", m.servers[0].config.Name)
	}
}

func TestUpdate_QuitKey(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	updated, cmd := m.Update(keyMsg)
	model := updated.(Model)
	if !model.quitting {
		t.Error("expected quitting to be true after 'q' key")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestUpdate_TabSwitching(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)

	// Tab forward
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(tabMsg)
	model := updated.(Model)
	if model.activeTab != 1 {
		t.Errorf("expected activeTab 1 after tab, got %d", model.activeTab)
	}

	// Tab wraps around
	updated, _ = model.Update(tabMsg)
	model = updated.(Model)
	if model.activeTab != 0 {
		t.Errorf("expected activeTab 0 after wrap, got %d", model.activeTab)
	}
}

func TestUpdate_ShiftTabSwitching(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)

	shiftTabMsg := tea.KeyMsg{Type: tea.KeyShiftTab}
	updated, _ := m.Update(shiftTabMsg)
	model := updated.(Model)
	if model.activeTab != 1 {
		t.Errorf("expected activeTab 1 after shift+tab wrap, got %d", model.activeTab)
	}
}

func TestUpdate_SingleServerTabNoop(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, []string{"rpi5"})

	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(tabMsg)
	model := updated.(Model)
	if model.activeTab != 0 {
		t.Errorf("single server tab should stay 0, got %d", model.activeTab)
	}
}

func TestUpdate_WindowResize(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)

	sizeMsg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := m.Update(sizeMsg)
	model := updated.(Model)
	if model.width != 120 || model.height != 40 {
		t.Errorf("expected 120x40, got %dx%d", model.width, model.height)
	}
}

func TestUpdate_DataMsg(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)

	data := ServerData{
		Name: "rpi5",
		Status: &system.StatusInfo{
			Hostname: "rpi5",
			CPU:      system.CPUInfo{UsagePercent: 42.5, Cores: 4},
			Memory:   system.MemInfo{TotalGB: 8, UsedGB: 4, Percent: 50},
		},
	}

	msg := dataMsg{index: 0, data: data}
	updated, _ := m.Update(msg)
	model := updated.(Model)
	if model.servers[0].data.Status == nil {
		t.Fatal("expected status data to be set")
	}
	if model.servers[0].data.Status.CPU.UsagePercent != 42.5 {
		t.Errorf("expected CPU 42.5, got %.1f", model.servers[0].data.Status.CPU.UsagePercent)
	}
}

func TestUpdate_DataMsgOutOfBounds(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)

	msg := dataMsg{index: 99, data: ServerData{Name: "ghost"}}
	updated, _ := m.Update(msg)
	_ = updated.(Model) // should not panic
}

func TestView_Loading(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)
	// width=0 means no WindowSizeMsg received yet
	v := m.View()
	if !strings.Contains(v, "Loading") {
		t.Error("expected loading message when width is 0")
	}
}

func TestView_Quitting(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)
	m.quitting = true
	v := m.View()
	if v != "" {
		t.Errorf("expected empty view when quitting, got %q", v)
	}
}

func TestView_WithData(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)
	m.width = 100
	m.height = 30

	m.servers[0].data = ServerData{
		Name: "rpi5",
		Status: &system.StatusInfo{
			Hostname: "rpi5",
			OS:       "linux",
			Arch:     "arm64",
			Uptime:   "5d 3h",
			CPU:      system.CPUInfo{UsagePercent: 45.2, Cores: 4},
			Memory:   system.MemInfo{TotalGB: 8, UsedGB: 4.2, Percent: 52.5},
			Disks:    []system.DiskInfo{{Mount: "/", TotalGB: 64, UsedGB: 30, Percent: 47}},
		},
		DockerStatus: "ok",
		Containers: []docker.Container{
			{Name: "nginx", State: "running", Image: "nginx:latest", Status: "Up 2 days"},
			{Name: "postgres", State: "running", Image: "postgres:16", Status: "Up 2 days"},
		},
		Alerts: &alerts.AlertResult{
			CPU:    alerts.AlertItem{Status: "ok", Current: 45.2, Threshold: 90},
			Memory: alerts.AlertItem{Status: "ok", Current: 52.5, Threshold: 85},
			Disks:  []alerts.DiskAlert{{Mount: "/", Status: "ok", Current: 47, Threshold: 90}},
		},
	}

	v := m.View()

	// Check tabs are rendered
	if !strings.Contains(v, "rpi5") {
		t.Error("expected 'rpi5' in tab bar")
	}

	// Check system panel content
	if !strings.Contains(v, "rpi5") {
		t.Error("expected server name in system panel")
	}
	if !strings.Contains(v, "CPU") {
		t.Error("expected CPU in system panel")
	}
	if !strings.Contains(v, "Uptime") {
		t.Error("expected Uptime in system panel")
	}
	if !strings.Contains(v, "5d 3h") {
		t.Error("expected uptime value '5d 3h'")
	}

	// Check docker panel
	if !strings.Contains(v, "Docker") {
		t.Error("expected 'Docker' title")
	}
	if !strings.Contains(v, "nginx") {
		t.Error("expected container 'nginx'")
	}
	if !strings.Contains(v, "postgres") {
		t.Error("expected container 'postgres'")
	}

	// Check footer
	if !strings.Contains(v, "quit") {
		t.Error("expected keybinding hints in footer")
	}
}

func TestView_WithError(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)
	m.width = 100
	m.height = 30
	m.servers[0].data = ServerData{
		Error: fmt.Errorf("ssh connect failed"),
	}

	v := m.View()
	if !strings.Contains(v, "ssh connect failed") {
		t.Error("expected error message in view")
	}
}

func TestView_EmptyContainers(t *testing.T) {
	cfg := testConfig()
	m := NewModel(cfg, nil)
	m.width = 100
	m.height = 30
	m.servers[0].data = ServerData{
		Name: "rpi5",
		Status: &system.StatusInfo{
			Hostname: "rpi5",
			CPU:      system.CPUInfo{UsagePercent: 10, Cores: 4},
			Memory:   system.MemInfo{TotalGB: 8, UsedGB: 2, Percent: 25},
		},
		DockerStatus: "ok",
		Containers:   []docker.Container{},
	}

	v := m.View()
	if !strings.Contains(v, "No containers") {
		t.Error("expected 'No containers' message")
	}
}

// -- Style helper tests --

func TestProgressBar(t *testing.T) {
	tests := []struct {
		percent float64
		width   int
	}{
		{0, 10},
		{50, 20},
		{75, 15},
		{95, 10},
		{100, 10},
	}
	for _, tt := range tests {
		bar := progressBar(tt.percent, tt.width)
		if bar == "" {
			t.Errorf("progressBar(%.0f, %d) returned empty", tt.percent, tt.width)
		}
	}
}

func TestProgressBar_MinWidth(t *testing.T) {
	bar := progressBar(50, 2)
	if bar == "" {
		t.Error("progressBar with small width should still render")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell~"},
		{"ab", 2, "ab"},
		{"abc", 2, "a~"},
		{"x", 1, "x"},
		{"xy", 1, "x"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

func TestAlertStyle(t *testing.T) {
	// Just ensure no panics and returns non-nil styles
	styles := []alerts.Level{"ok", "warning", "critical", "unknown"}
	for _, s := range styles {
		style := alertStyle(s)
		_ = style.Render("test")
	}
}
