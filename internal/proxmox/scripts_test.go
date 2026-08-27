package proxmox

import (
	"strings"
	"testing"
)

func TestScriptsCatalogHasNoDuplicateSlugs(t *testing.T) {
	seen := make(map[string]bool, len(scriptCatalog))
	for _, script := range Scripts() {
		if seen[script.Slug] {
			t.Errorf("duplicate script slug %q", script.Slug)
		}
		seen[script.Slug] = true
		if script.Name == "" || script.Description == "" {
			t.Errorf("script %q missing name or description: %#v", script.Slug, script)
		}
	}
}

func TestScriptCommandPinsRefAndSlug(t *testing.T) {
	cmd, err := ScriptCommand("docker")
	if err != nil {
		t.Fatal(err)
	}
	want := `bash -c "$(curl -fsSL https://raw.githubusercontent.com/community-scripts/ProxmoxVE/` + scriptCatalogRef + `/ct/docker.sh)"`
	if cmd != want {
		t.Errorf("ScriptCommand(docker) = %q, want %q", cmd, want)
	}
	if strings.Contains(cmd, "main/ct") {
		t.Errorf("ScriptCommand must pin a commit, not track main: %q", cmd)
	}
}

func TestScriptCommandUnknownSlug(t *testing.T) {
	if _, err := ScriptCommand("nope"); err == nil || !strings.Contains(err.Error(), `unknown Proxmox Community Script "nope"`) {
		t.Errorf("ScriptCommand(nope) error = %v", err)
	}
}
