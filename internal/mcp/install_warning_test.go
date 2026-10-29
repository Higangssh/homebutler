package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/install"
)

// An earlier filebrowser install whose container is gone left its database in
// an anonymous volume (#327). The CLI warns on stderr, and an agent was told
// nothing: install_app said installed and install_uninstall said
// data_preserved: true. Both now carry the warning.
func TestInstallAndUninstallSayWhenAnEarlierInstallsStateIsStranded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origInstall, origUninstall, origPre := installApp, uninstallApp, preCheckApp
	t.Cleanup(func() { installApp, uninstallApp, preCheckApp = origInstall, origUninstall, origPre })
	preCheckApp = func(install.App, string) []string { return nil }
	stranded := install.Carryover{Stranded: []string{"/config", "/database"}}

	s := NewServer(&config.Config{}, "test")

	installApp = func(install.App, install.InstallOptions) (install.Carryover, error) { return stranded, nil }
	got := toolAnswer(t, s, "install_app")
	if w, _ := got["warning"].(string); !strings.Contains(w, "/database") || !strings.Contains(w, "started without them") {
		t.Errorf("install_app warning = %#v", got["warning"])
	}

	uninstallApp = func(string) (install.Carryover, error) { return stranded, nil }
	got = toolAnswer(t, s, "install_uninstall")
	if w, _ := got["warning"].(string); !strings.Contains(w, "did not touch them") {
		t.Errorf("install_uninstall warning = %#v", got["warning"])
	}
	// data_preserved names the operation (#353), and the operation kept
	// everything it found.
	if got["data_preserved"] != true {
		t.Errorf("data_preserved = %#v, want true", got["data_preserved"])
	}

	// Nothing stranded: the answer has the shape it always had.
	installApp = func(install.App, install.InstallOptions) (install.Carryover, error) {
		return install.Carryover{Copied: []string{"/x/database"}}, nil
	}
	uninstallApp = func(string) (install.Carryover, error) { return install.Carryover{}, nil }
	for _, tool := range []string{"install_app", "install_uninstall"} {
		if w, ok := toolAnswer(t, s, tool)["warning"]; ok {
			t.Errorf("%s answered with warning = %#v when nothing was stranded", tool, w)
		}
	}
}

func toolAnswer(t *testing.T, s *Server, tool string) map[string]any {
	t.Helper()
	result, err := s.executeTool(tool, map[string]any{"app": "filebrowser"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
