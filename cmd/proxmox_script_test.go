package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestProxmoxScriptListJSON(t *testing.T) {
	oldJSON := jsonOutput
	defer func() { jsonOutput = oldJSON }()
	jsonOutput = true

	cmd := newProxmoxScriptListCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var scripts []struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(output.Bytes(), &scripts); err != nil {
		t.Fatalf("decode script list JSON: %v\n%s", err, output.String())
	}
	if len(scripts) == 0 {
		t.Fatal("script list is empty")
	}
	found := false
	for _, s := range scripts {
		if s.Slug == "docker" {
			found = true
		}
	}
	if !found {
		t.Errorf("script list = %#v, want a docker entry", scripts)
	}
}

func TestProxmoxScriptShowNeverExecutes(t *testing.T) {
	oldJSON := jsonOutput
	defer func() { jsonOutput = oldJSON }()
	jsonOutput = false

	cmd := newProxmoxScriptShowCmd()
	cmd.SetArgs([]string{"docker"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"Slug: docker", "curl -fsSL https://raw.githubusercontent.com/community-scripts/ProxmoxVE/", "does not run it for you", "not reviewed by homebutler", "runs as root"} {
		if !strings.Contains(got, want) {
			t.Errorf("script show output missing %q:\n%s", want, got)
		}
	}
}

func TestProxmoxScriptShowUnknownSlug(t *testing.T) {
	cmd := newProxmoxScriptShowCmd()
	cmd.SetArgs([]string{"nope"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), `unknown Proxmox Community Script "nope"`) {
		t.Fatalf("script show error = %v", err)
	}
}

func TestProxmoxScriptShowJSON(t *testing.T) {
	oldJSON := jsonOutput
	defer func() { jsonOutput = oldJSON }()
	jsonOutput = true

	cmd := newProxmoxScriptShowCmd()
	cmd.SetArgs([]string{"pihole"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result proxmoxScriptCommandResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Slug != "pihole" || !strings.Contains(result.Command, "/ct/pihole.sh") || result.Warning == "" {
		t.Errorf("script show result = %#v", result)
	}
}
