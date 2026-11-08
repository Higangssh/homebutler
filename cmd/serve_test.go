package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/server"
	"github.com/Higangssh/homebutler/internal/service"
)

// An installed dashboard outlives the attention of the person who installed
// it, so binding it where other machines can reach it has to be a decision
// somebody made rather than a default they inherited.
func TestInstallRefusesAReachableBindWithNoToken(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "192.168.1.10", "::"} {
		err := installBindRefusal(host, "", "/home/x/.homebutler/config.yaml")
		if err == nil {
			t.Errorf("installing on %s with no token was allowed", host)
			continue
		}
		// Which fix is right depends on what the operator meant, so refusing
		// is only useful if it names both.
		if !strings.Contains(err.Error(), "web.token") || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("refusal for %s names only one way out: %v", host, err)
		}
	}
}

func TestInstallAllowsLoopbackAndTokenedBinds(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "::1"} {
		if err := installBindRefusal(host, "", "/c.yaml"); err != nil {
			t.Errorf("installing on %s with no token was refused: %v", host, err)
		}
	}
	if err := installBindRefusal("0.0.0.0", "s3cret", "/c.yaml"); err != nil {
		t.Errorf("installing on 0.0.0.0 with a token was refused: %v", err)
	}
}

// --token is visible in ps to every user on the machine and a unit file is
// world-readable, so the token reaches the dashboard through the config file
// and through nothing else.
func TestTheServeUnitCarriesNoToken(t *testing.T) {
	const secret = "tok_do_not_write_this_down"
	unit := service.Serve("127.0.0.1", 9090)
	for _, kind := range []service.Kind{service.Systemd, service.Launchd} {
		rendered := service.Render(kind, "/usr/local/bin/homebutler", "/home/x", unit)
		if strings.Contains(rendered, secret) || strings.Contains(rendered, "--token") {
			t.Errorf("%s unit mentions a token:\n%s", kind, rendered)
		}
	}
}

// env fakes the process environment for resolveWebToken, so a test can say
// "set but empty" without touching the real one.
func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestTheTokenComesFromTheFlagThenTheConfigThenTheEnvironment(t *testing.T) {
	withConfig := &config.Config{Path: "/config/config.yaml", Web: config.WebConfig{Token: "from-config"}}
	noToken := &config.Config{Path: "/config/config.yaml"}
	fromEnv := env(map[string]string{"HOMEBUTLER_TOKEN": "from-env"})
	unset := env(nil)

	cases := []struct {
		name   string
		flag   string
		cfg    *config.Config
		lookup func(string) (string, bool)
		want   string
	}{
		{"the flag wins over everything", "from-flag", withConfig, fromEnv, "from-flag"},
		{"the config wins over the environment", "", withConfig, fromEnv, "from-config"},
		{"the environment fills in when nothing else is set", "", noToken, fromEnv, "from-env"},
		{"the environment works with no config at all", "", nil, fromEnv, "from-env"},
		{"no source is no token", "", noToken, unset, ""},
		{"no config and no variable is no token", "", nil, unset, ""},
		{"the config alone still works", "", withConfig, unset, "from-config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := resolveWebToken(tc.flag, tc.cfg, tc.lookup); got != tc.want {
				t.Errorf("resolveWebToken = %q, want %q", got, tc.want)
			}
		})
	}
}

// An unfilled template field arrives as HOMEBUTLER_TOKEN= — set, and empty.
// Taking that as the token would mean "no authentication", which is the one
// thing nobody filling in a token field asked for.
func TestAnEmptyTokenVariableIsUnsetAndSaysSo(t *testing.T) {
	for _, value := range []string{"", "   ", "\n"} {
		token, note := resolveWebToken("", &config.Config{}, env(map[string]string{"HOMEBUTLER_TOKEN": value}))
		if token != "" {
			t.Errorf("HOMEBUTLER_TOKEN=%q gave token %q, want none", value, token)
		}
		if !strings.Contains(note, "HOMEBUTLER_TOKEN") || !strings.Contains(note, "empty") {
			t.Errorf("HOMEBUTLER_TOKEN=%q gave note %q, want it to say the variable is empty", value, note)
		}
	}
}

func TestAShadowedTokenVariableIsNamed(t *testing.T) {
	cfg := &config.Config{Path: "/config/config.yaml", Web: config.WebConfig{Token: "from-config"}}
	_, note := resolveWebToken("", cfg, env(map[string]string{"HOMEBUTLER_TOKEN": "from-env"}))
	if !strings.Contains(note, "HOMEBUTLER_TOKEN") || !strings.Contains(note, "/config/config.yaml") {
		t.Errorf("note = %q, want it to name the variable and the config it lost to", note)
	}
	// The same value in both places is not a conflict worth a line on stderr.
	cfg.Web.Token = "same"
	if _, note := resolveWebToken("", cfg, env(map[string]string{"HOMEBUTLER_TOKEN": "same"})); note != "" {
		t.Errorf("note = %q for an agreeing variable, want none", note)
	}
}

// The dashboard's settings screen writes config.yaml back. A token that came
// from the environment has to stay out of it, and out of everything the API
// answers with: the file is the one place the operator did not put it.
func TestAnEnvironmentTokenIsNeitherSavedNorServed(t *testing.T) {
	const secret = "env-only-secret-7f3a"
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("alerts:\n  cpu: 90\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	token, _ := resolveWebToken("", loaded, env(map[string]string{"HOMEBUTLER_TOKEN": secret}))
	if token != secret {
		t.Fatalf("resolveWebToken = %q, want the variable", token)
	}
	if loaded.Web.Token != "" {
		t.Fatalf("cfg.Web.Token = %q after resolving, want the config left alone", loaded.Web.Token)
	}

	srv := server.New(loaded, "127.0.0.1", 8080)
	srv.SetToken(token)
	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s %s echoed the token: %s", method, target, w.Body.String())
		}
		return w
	}

	var page struct {
		Revision string `json:"revision"`
		Editable bool   `json:"editable"`
	}
	if err := json.Unmarshal(call("GET", "/api/config", "").Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Editable || page.Revision == "" {
		t.Fatalf("the settings screen is not editable with the environment token: %+v", page)
	}
	if w := call("PUT", "/api/config/alerts", fmt.Sprintf(`{"revision":%q,"cpu":75}`, page.Revision)); w.Code != http.StatusOK {
		t.Fatalf("save = %d %s", w.Code, w.Body.String())
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "75") {
		t.Fatalf("the save did not reach the file:\n%s", saved)
	}
	if strings.Contains(string(saved), secret) || strings.Contains(string(saved), "token") {
		t.Errorf("config.yaml carries the environment token after a save:\n%s", saved)
	}
}
