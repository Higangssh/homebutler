package capability

import (
	"strings"
	"testing"
)

// The failure this guards is silent: a capability added to the registry shows
// up in MCP immediately and in the browser never, and nothing said which of
// those was meant.
func TestEveryCapabilityRecordsItsHTTPDecision(t *testing.T) {
	for _, c := range Registry {
		switch {
		case c.HTTP.Method != "" && c.HTTP.Absent != "":
			t.Errorf("%s is both exposed and absent", c.Tool.Name)
		case c.HTTP.Method == "" && c.HTTP.Absent == "":
			t.Errorf("%s is not on the HTTP surface and does not say why", c.Tool.Name)
		case c.HTTP.Method != "" && c.HTTP.Path == "":
			t.Errorf("%s declares a method with no path", c.Tool.Name)
		}
	}
}

func TestExposedPathsAreDistinctAPIRoutes(t *testing.T) {
	seen := map[string]string{}
	for _, c := range Registry {
		if !c.Exposed() {
			continue
		}
		if !strings.HasPrefix(c.HTTP.Path, "/api/") {
			t.Errorf("%s is exposed at %q, which is not under /api/", c.Tool.Name, c.HTTP.Path)
		}
		route := c.HTTP.Method + " " + c.HTTP.Path
		if other, ok := seen[route]; ok {
			t.Errorf("%s and %s both claim %s", other, c.Tool.Name, route)
		}
		seen[route] = c.Tool.Name
	}
}

// Risk is what decides whether an operation needs a token and a confirmation,
// so a capability without one is a capability nothing can gate.
func TestEveryCapabilityDeclaresRiskAndTargets(t *testing.T) {
	for _, c := range Registry {
		switch c.Risk {
		case RiskRead, RiskWrite, RiskDestructive:
		default:
			t.Errorf("%s has risk %q", c.Tool.Name, c.Risk)
		}
		if len(c.Targets) == 0 {
			t.Errorf("%s can be pointed at nothing", c.Tool.Name)
		}
	}
}

// Everything a browser can reach today is a read, apart from wake, which sends
// a magic packet and changes nothing homebutler stores. The write surface and
// the token rule behind it are #154; if this starts failing, that decision is
// being made here by accident.
func TestNothingDestructiveIsExposedYet(t *testing.T) {
	for _, c := range Registry {
		if !c.Exposed() {
			continue
		}
		if c.Risk == RiskDestructive {
			t.Errorf("%s is destructive and reachable from a browser", c.Tool.Name)
		}
		if c.Risk == RiskWrite && c.Tool.Name != "wake" {
			t.Errorf("%s is a write and reachable from a browser before #154 decided how", c.Tool.Name)
		}
	}
}
