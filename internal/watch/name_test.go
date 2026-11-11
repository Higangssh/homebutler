package watch

import (
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, name := range []string{"web", "nginx.service", "my-api", "a_b", "x-", strings.Repeat("a", 128)} {
		if !ValidName(name) {
			t.Errorf("%q refused", name)
		}
	}
	// A leading '-' is an option to docker, systemctl and pm2:
	// `systemctl restart --help` exits 0 and was counted as a restart.
	for _, name := range []string{"", "--help", "-H", "-", "a b", "a;b", "a/b", "web@1", strings.Repeat("a", 129)} {
		if ValidName(name) {
			t.Errorf("%q accepted", name)
		}
	}
}

// The rule is for adding. A list written before it keeps loading, so a
// target stored with a leading '-' does not break the watch list it is on.
func TestAStoredNameTheRuleRefusesStillLoads(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTargets(dir, []Target{{Container: "-old", Kind: KindSystemd, Unit: "--help"}, {Container: "web", Kind: KindPM2}}); err != nil {
		t.Fatal(err)
	}
	targets, err := LoadTargets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Container != "-old" {
		t.Fatalf("targets = %+v", targets)
	}
	removed, err := RemoveTarget(dir, "-old")
	if err != nil || !removed {
		t.Fatalf("could not remove the stored target: %v, %v", removed, err)
	}
}
