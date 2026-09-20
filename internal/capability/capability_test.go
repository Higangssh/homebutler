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

// A capability may be reachable from a browser only with the protection its
// risk level calls for. #154 replaced the blanket ban this used to be — the
// dashboard is meant to manage things now — with the rule that made exposing
// them acceptable: a read needs nothing, a write needs a token, and something
// destructive needs a token and a confirmation the caller has to send.
//
// The test is here rather than in internal/server because it is a property of
// the registry: an entry that claims a browser can reach it has to say how it
// is protected, and the protection has to match what the entry itself says it
// costs to call.
func TestExposedCapabilitiesCarryTheProtectionTheirRiskNeeds(t *testing.T) {
	for _, c := range Registry {
		if !c.Exposed() {
			if c.HTTP.Protection != "" {
				t.Errorf("%s is not exposed and names a protection", c.Tool.Name)
			}
			continue
		}

		switch c.Risk {
		case RiskRead:
			if c.HTTP.Protection != ProtectionNone {
				t.Errorf("%s is a read and asks for %q", c.Tool.Name, c.HTTP.Protection)
			}
		case RiskWrite:
			if c.HTTP.Protection != ProtectionToken {
				t.Errorf("%s is a write reachable from a browser and is protected by %q, not a token", c.Tool.Name, c.HTTP.Protection)
			}
		case RiskDestructive:
			if c.HTTP.Protection != ProtectionTokenAndConfirm {
				t.Errorf("%s is destructive and reachable from a browser without a confirmation", c.Tool.Name)
			}
		}
	}
}

// wake is the one write a browser could always reach, because it sends a magic
// packet and changes nothing homebutler stores. It still needs the token.
// The dashboard's write surface is kept as a list rather than a rule, so that
// putting something on it is a decision somebody wrote down and defended. A
// write that becomes reachable without appearing here fails this.
func TestEveryExposedWriteIsOneWeChose(t *testing.T) {
	chosen := map[string]string{
		"wake":        "the wake button; sends a magic packet on the local network",
		"notify_test": "the settings screen's test button; sends one message through channels the operator configured",
		"report":      "the Report tab's save button; writes a snapshot, which moves the window the next comparison covers. Reading the comparison is GET /api/report, which saves nothing",
	}

	for _, c := range Registry {
		if !c.Exposed() || c.Risk != RiskWrite {
			continue
		}
		if _, ok := chosen[c.Tool.Name]; !ok {
			t.Errorf("%s is an exposed write that is not on the list; decide whether the dashboard should reach it", c.Tool.Name)
			continue
		}
		// Every one of them costs a token: a write reachable by anyone who can
		// load the page is the thing this list exists to prevent.
		if c.HTTP.Protection == ProtectionNone {
			t.Errorf("%s is an exposed write with no protection", c.Tool.Name)
		}
	}
}

// The reason an absence gives goes out of `GET /api/capabilities`, so it is a
// sentence said to whoever asks what the dashboard can do. A reason written at
// the call site would be prose nobody could count, and the question this
// registry exists to answer — how much is the HTTP surface still going to
// grow — cannot be answered by prose.
func TestAbsentReasonsAreFromTheClosedSet(t *testing.T) {
	for _, c := range Registry {
		if c.Exposed() {
			continue
		}
		if c.HTTP.Absent == "" {
			t.Errorf("%s is not exposed and does not say why", c.Tool.Name)
			continue
		}
		if !absentReasons[c.HTTP.Absent] {
			t.Errorf("%s gives a reason that is not one of the constants: %q", c.Tool.Name, c.HTTP.Absent)
		}
	}
}

// A destructive capability and a write one are absent for different reasons
// and are waiting on different decisions. Letting them share a reason is how
// "the dashboard can restart a container" and "the dashboard can delete an
// app's data" become one question, which is the question #242 exists to keep
// apart.
func TestDestructiveAbsencesNameTheDestructiveDecision(t *testing.T) {
	for _, c := range Registry {
		if c.Exposed() {
			continue
		}
		switch c.Risk {
		case RiskDestructive:
			if c.HTTP.Absent != AbsentNoDestructiveRuleYet {
				t.Errorf("%s is destructive and waits on %q", c.Tool.Name, c.HTTP.Absent)
			}
		case RiskRead:
			if c.HTTP.Absent != AbsentNoViewYet {
				t.Errorf("%s is a read and waits on %q", c.Tool.Name, c.HTTP.Absent)
			}
		case RiskWrite:
			if c.HTTP.Absent != AbsentNoActionRuleYet {
				t.Errorf("%s is a write and waits on %q", c.Tool.Name, c.HTTP.Absent)
			}
		}
	}
}
