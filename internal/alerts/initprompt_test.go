package alerts

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/docker"
	"gopkg.in/yaml.v3"
)

func fakeContainers() ([]docker.Container, error) {
	return []docker.Container{
		{Name: "nginx", State: "running"},
		{Name: "redis", State: "running"},
		{Name: "postgres", State: "exited"},
	}, nil
}

func noContainers() ([]docker.Container, error) {
	return nil, nil
}

func TestRunInitPrompt_Defaults(t *testing.T) {
	// All defaults: Enter x3, then "all", confirm "y", then "1" (restart), then empty webhook
	input := "\n\n\nall\n\n1\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	result, err := RunInitPrompt(r, &w, fakeContainers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPUThreshold != 90 {
		t.Errorf("CPU threshold: got %v, want 90", result.CPUThreshold)
	}
	if result.MemoryThreshold != 85 {
		t.Errorf("Memory threshold: got %v, want 85", result.MemoryThreshold)
	}
	if result.DiskThreshold != 85 {
		t.Errorf("Disk threshold: got %v, want 85", result.DiskThreshold)
	}
	if len(result.Containers) != 3 {
		t.Errorf("Containers: got %d, want 3", len(result.Containers))
	}
	if result.ContainerAction != "restart" {
		t.Errorf("Action: got %q, want restart", result.ContainerAction)
	}
	if result.WebhookURL != "" {
		t.Errorf("Webhook: got %q, want empty", result.WebhookURL)
	}
}

func TestRunInitPrompt_CustomValues(t *testing.T) {
	input := "80\n70\n60\n1,3\ny\n2\nhttps://example.com/hook\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	result, err := RunInitPrompt(r, &w, fakeContainers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPUThreshold != 80 {
		t.Errorf("CPU threshold: got %v, want 80", result.CPUThreshold)
	}
	if result.MemoryThreshold != 70 {
		t.Errorf("Memory threshold: got %v, want 70", result.MemoryThreshold)
	}
	if result.DiskThreshold != 60 {
		t.Errorf("Disk threshold: got %v, want 60", result.DiskThreshold)
	}
	if len(result.Containers) != 2 || result.Containers[0] != "nginx" || result.Containers[1] != "postgres" {
		t.Errorf("Containers: got %v, want [nginx postgres]", result.Containers)
	}
	if result.ContainerAction != "notify" {
		t.Errorf("Action: got %q, want notify", result.ContainerAction)
	}
	if result.WebhookURL != "https://example.com/hook" {
		t.Errorf("Webhook: got %q, want https://example.com/hook", result.WebhookURL)
	}
}

func TestRunInitPrompt_NoDocker(t *testing.T) {
	dockerErr := func() ([]docker.Container, error) {
		return nil, fmt.Errorf("docker is not installed")
	}

	// 3 thresholds + webhook
	input := "\n\n\nhttps://slack.test\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	result, err := RunInitPrompt(r, &w, dockerErr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Containers) != 0 {
		t.Errorf("Containers should be empty, got %v", result.Containers)
	}
	if result.WebhookURL != "https://slack.test" {
		t.Errorf("Webhook: got %q", result.WebhookURL)
	}
}

func TestRunInitPrompt_NoContainersFound(t *testing.T) {
	input := "\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	result, err := RunInitPrompt(r, &w, noContainers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Containers) != 0 {
		t.Errorf("Containers should be empty, got %v", result.Containers)
	}
}

func TestBuildYAML(t *testing.T) {
	res := &InitResult{
		CPUThreshold:    90,
		MemoryThreshold: 85,
		DiskThreshold:   85,
		Containers:      []string{"nginx", "redis"},
		ContainerAction: "restart",
		WebhookURL:      "https://example.com/hook",
	}

	yamlStr, err := BuildYAML(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(yamlStr, "cpu-spike") {
		t.Error("YAML should contain cpu-spike rule")
	}
	if !strings.Contains(yamlStr, "container-down") {
		t.Error("YAML should contain container-down rule")
	}
	if !strings.Contains(yamlStr, "https://example.com/hook") {
		t.Error("YAML should contain webhook URL")
	}

	// Verify the generated YAML is valid by loading it
	cfg := UserConfig{}
	if err := loadYAMLString(yamlStr, &cfg); err != nil {
		t.Fatalf("generated YAML is invalid: %v", err)
	}
	if len(cfg.Alerts.Rules) != 4 {
		t.Errorf("Expected 4 rules, got %d", len(cfg.Alerts.Rules))
	}
}

func TestBuildYAML_NoContainers(t *testing.T) {
	res := &InitResult{
		CPUThreshold:    90,
		MemoryThreshold: 85,
		DiskThreshold:   85,
	}

	yamlStr, err := BuildYAML(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(yamlStr, "container-down") {
		t.Error("YAML should not contain container-down rule when no containers selected")
	}
}

func TestParseContainerSelection(t *testing.T) {
	containers := []docker.Container{
		{Name: "nginx"},
		{Name: "redis"},
		{Name: "postgres"},
	}

	tests := []struct {
		input string
		want  []string
	}{
		{"all", []string{"nginx", "redis", "postgres"}},
		{"ALL", []string{"nginx", "redis", "postgres"}},
		{"", []string{"nginx", "redis", "postgres"}},
		{"1", []string{"nginx"}},
		{"1,3", []string{"nginx", "postgres"}},
		{"2, 1", []string{"redis", "nginx"}},
		{"1,1", []string{"nginx"}}, // dedup
		{"99", nil},                // out of range
	}

	for _, tt := range tests {
		got := parseContainerSelection(tt.input, containers)
		if len(got) != len(tt.want) {
			t.Errorf("parseContainerSelection(%q): got %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseContainerSelection(%q)[%d]: got %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// helper: unmarshal YAML string
func loadYAMLString(s string, cfg interface{}) error {
	return yaml.Unmarshal([]byte(s), cfg)
}
