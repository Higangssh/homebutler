package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/install"
)

// The dashboard's install and uninstall routes say what install_app and
// install_uninstall say about an earlier install's stranded state (#327).
func TestInstallRoutesSayWhenAnEarlierInstallsStateIsStranded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origInstall, origUninstall, origPre := installApp, uninstallApp, preCheckApp
	t.Cleanup(func() { installApp, uninstallApp, preCheckApp = origInstall, origUninstall, origPre })
	preCheckApp = func(install.App, string) []string { return nil }
	stranded := install.Carryover{Stranded: []string{"/config", "/database"}}

	installApp = func(install.App, install.InstallOptions) (install.Carryover, error) { return stranded, nil }
	got := routeAnswer(t, "install", (&Server{}).handleInstallApp)
	if w, _ := got["warning"].(string); !strings.Contains(w, "/database") || !strings.Contains(w, "started without them") {
		t.Errorf("install route warning = %#v", got["warning"])
	}

	uninstallApp = func(string) (install.Carryover, error) { return stranded, nil }
	got = routeAnswer(t, "uninstall", (&Server{}).handleInstallUninstall)
	if w, _ := got["warning"].(string); !strings.Contains(w, "did not touch them") {
		t.Errorf("uninstall route warning = %#v", got["warning"])
	}
	if got["data_preserved"] != true {
		t.Errorf("data_preserved = %#v, want true", got["data_preserved"])
	}

	installApp = func(install.App, install.InstallOptions) (install.Carryover, error) { return install.Carryover{}, nil }
	uninstallApp = func(string) (install.Carryover, error) { return install.Carryover{}, nil }
	if w, ok := routeAnswer(t, "install", (&Server{}).handleInstallApp)["warning"]; ok {
		t.Errorf("install route answered with warning = %#v when nothing was stranded", w)
	}
	if w, ok := routeAnswer(t, "uninstall", (&Server{}).handleInstallUninstall)["warning"]; ok {
		t.Errorf("uninstall route answered with warning = %#v when nothing was stranded", w)
	}
}

func routeAnswer(t *testing.T, action string, handler http.HandlerFunc) map[string]any {
	t.Helper()
	path := "/api/install/filebrowser"
	if action != "install" {
		path += "/" + action
	}
	req := httptest.NewRequest("POST", path, nil)
	req.SetPathValue("app", "filebrowser")
	w := httptest.NewRecorder()
	handler(w, req)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return got
}
