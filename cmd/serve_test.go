package cmd

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
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

func TestTheTokenComesFromTheConfigWhenTheFlagIsEmpty(t *testing.T) {
	cfg := &config.Config{Web: config.WebConfig{Token: "from-config"}}
	if got := resolveWebToken("", cfg); got != "from-config" {
		t.Errorf("resolveWebToken(\"\", cfg) = %q, want from-config", got)
	}
	if got := resolveWebToken("from-flag", cfg); got != "from-flag" {
		t.Errorf("resolveWebToken with a flag = %q, want the flag to win", got)
	}
	if got := resolveWebToken("", nil); got != "" {
		t.Errorf("resolveWebToken with no config = %q, want empty", got)
	}
}
