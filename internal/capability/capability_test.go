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
			// Not "is there a gate" but "is it the gate this one's tier
			// calls for". A destructive action that can be undone by doing
			// something else takes a second confirmation; one that cannot
			// takes the target's name, because a click cannot say which
			// thing the operator meant to lose.
			want := destructiveTier[c.Tool.Name]
			if want == "" {
				t.Errorf("%s is destructive and reachable from a browser and no tier was decided for it", c.Tool.Name)
				continue
			}
			if c.HTTP.Protection != want {
				t.Errorf("%s is tier %q and its route asks for %q", c.Tool.Name, want, c.HTTP.Protection)
			}
		}
	}
}

// destructiveTier records which of the two destructive tiers each one is in,
// decided in #242 by whether doing something else undoes it. Keeping it here
// rather than reading it back off the route is the point: the test would pass
// trivially if it asked the registry what the registry says.
var destructiveTier = map[string]Protection{
	// Reversible: the service comes back when it is started again.
	"docker_stop":            ProtectionTokenAndConfirm,
	"proxmox_guest_shutdown": ProtectionTokenAndConfirm,
	// Not reversible: the data is gone.
	"backup_restore": ProtectionTokenAndName,
	"install_purge":  ProtectionTokenAndName,
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

		// #263: the action tier. Every one of these is undone by doing
		// something else, which is why none of them asks twice.
		"docker_restart":       "the container card's restart button; the container comes back",
		"backup_create":        "the backup screen; an extra archive is an extra file",
		"backup_drill":         "the backup screen; boots a copy beside the live app and removes it either way",
		"install_app":          "the app catalogue; shows its pre-flight and the host port it will bind before it runs",
		"install_uninstall":    "the app screen; stops the app and leaves its data, which install_purge is the one that does not",
		"watch_add":            "the watch screen; writes the list, and the supervisor is a separate install the operator does",
		"watch_remove":         "the watch screen; the incidents it recorded stay",
		"watch_check":          "the watch screen's check button; reads the targets and records what it found",
		"proxmox_guest_start":  "the Proxmox screen; a started guest can be shut down",
		"proxmox_guest_reboot": "the Proxmox screen; the guest comes back",
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

// The tiers go out of GET /api/capabilities and are frozen at 1.0, so a tier
// written at the call site would be a promise nobody could count. Same closed
// set as the absence reasons, for the same reason.
func TestProtectionsAreFromTheClosedSet(t *testing.T) {
	for _, c := range Registry {
		if !protections[c.HTTP.Protection] {
			t.Errorf("%s asks for %q, which is not one of the tiers", c.Tool.Name, c.HTTP.Protection)
		}
	}
}

// A route that acts on something it has to be told the name of has to be able
// to find that name here. An endpoint that takes an identifier, with no
// endpoint that produces one, is half an API: our own dashboard knows the
// value because it fetched it from somewhere, and nobody else has a somewhere.
//
// backup_restore is why Target is declared rather than derived. Its archive
// name arrives in the body, so a rule that reads paths for "{...}" would walk
// straight past the one route where the gap was worst — backup_list had no
// HTTP route at all, so there was no way through this API to learn the name of
// an archive to restore.
func TestARouteThatTakesATargetCanFindOne(t *testing.T) {
	exposed := map[string]Capability{}
	for _, c := range Registry {
		if c.Exposed() {
			exposed[c.Tool.Name] = c
		}
	}

	for _, c := range Registry {
		if c.HTTP.Target == "" {
			continue
		}
		if c.HTTP.Target == TargetFromConfig {
			// Served by GET /api/wake, which is not a capability. Nothing
			// here can check that route exists; internal/contract records it.
			continue
		}
		source, ok := exposed[c.HTTP.Target]
		if !ok {
			t.Errorf("%s takes a target from %q, which is not reachable over HTTP: a caller has no way to learn the value this route requires",
				c.Tool.Name, c.HTTP.Target)
			continue
		}
		if source.HTTP.Method != "GET" {
			t.Errorf("%s takes a target from %s, which is %s and not a read",
				c.Tool.Name, c.HTTP.Target, source.HTTP.Method)
		}
	}
}

// The half of the rule that can be checked mechanically: an identifier in the
// path is visible, so forgetting to say where it comes from is catchable.
// An identifier in the body is not — see the comment above.
func TestEveryPathParameterSaysWhereItComesFrom(t *testing.T) {
	for _, c := range Registry {
		if !c.Exposed() || !strings.Contains(c.HTTP.Path, "{") {
			continue
		}
		if c.HTTP.Target == "" {
			t.Errorf("%s is %s and names no Target: a caller cannot know what to put in place of the {...}",
				c.Tool.Name, c.HTTP.Path)
		}
	}
}
