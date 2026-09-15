package config

import (
	"os"
	"strings"
	"testing"
)

// The wake list is the first sequence the editor can write to, and these are
// the things a list needs that a mapping does not: a new item has to open with
// a dash, an item is addressed by the name its owner gave it rather than by
// where it sits, and removing one has to leave the ones around it alone.

const wakeFixture = `# hand written
wake:
  - name: gaming-pc
    mac: AA:BB:CC:DD:EE:FF   # the one in the study
    ip: 192.168.1.255
  - name: nas
    mac: 11:22:33:44:55:66

alerts:
  cpu: 90
`

func TestSaveChangesOneTargetsAddress(t *testing.T) {
	path := writeSaveFixture(t, wakeFixture)
	saveOne(t, path, Patch{Wake: []WakePatch{{Name: "nas", MAC: "11:22:33:44:55:77"}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if got := cfg.FindWakeTarget("nas"); got == nil || got.MAC != "11:22:33:44:55:77" {
		t.Fatalf("the address did not change: %+v", got)
	}
	if got := cfg.FindWakeTarget("gaming-pc"); got == nil || got.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("the other target changed: %+v", got)
	}

	saved := readFile(t, path)
	if !strings.Contains(saved, "# the one in the study") {
		t.Fatalf("the comment beside the other target is gone:\n%s", saved)
	}
}

func TestSaveAddsATargetToAListThatHasOne(t *testing.T) {
	path := writeSaveFixture(t, wakeFixture)
	saveOne(t, path, Patch{Wake: []WakePatch{{Name: "printer", MAC: "99:88:77:66:55:44"}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if len(cfg.Wake) != 3 {
		t.Fatalf("expected three targets, got %d: %+v", len(cfg.Wake), cfg.Wake)
	}
	if got := cfg.FindWakeTarget("printer"); got == nil || got.MAC != "99:88:77:66:55:44" {
		t.Fatalf("the new target is wrong: %+v", got)
	}
	// Written into the list rather than as a second wake key.
	if saved := readFile(t, path); strings.Count(saved, "wake:") != 1 {
		t.Fatalf("the file has more than one wake section:\n%s", saved)
	}
}

func TestSaveAddsTheFirstTargetOfAll(t *testing.T) {
	path := writeSaveFixture(t, "alerts:\n  cpu: 90\n")
	saveOne(t, path, Patch{Wake: []WakePatch{{Name: "nas", MAC: "11:22:33:44:55:66", Broadcast: "192.168.1.255"}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	got := cfg.FindWakeTarget("nas")
	if got == nil || got.MAC != "11:22:33:44:55:66" || got.Broadcast != "192.168.1.255" {
		t.Fatalf("the target did not round-trip: %+v", got)
	}
}

// A key an item never had is written into the item, not beside the list.
func TestSaveAddsAKeyToATargetThatLacksIt(t *testing.T) {
	path := writeSaveFixture(t, wakeFixture)
	saveOne(t, path, Patch{Wake: []WakePatch{{Name: "nas", MAC: "11:22:33:44:55:66", Broadcast: "10.0.0.255"}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if got := cfg.FindWakeTarget("nas"); got == nil || got.Broadcast != "10.0.0.255" {
		t.Fatalf("the broadcast is not on the target: %+v", got)
	}
	if got := cfg.FindWakeTarget("gaming-pc"); got == nil || got.Broadcast != "192.168.1.255" {
		t.Fatalf("the other target's broadcast changed: %+v", got)
	}
}

func TestSaveRemovesOneTargetAndLeavesTheRest(t *testing.T) {
	path := writeSaveFixture(t, wakeFixture)
	saveOne(t, path, Patch{Wake: []WakePatch{{Name: "gaming-pc", Remove: true}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if cfg.FindWakeTarget("gaming-pc") != nil {
		t.Fatal("the target was not removed")
	}
	if got := cfg.FindWakeTarget("nas"); got == nil || got.MAC != "11:22:33:44:55:66" {
		t.Fatalf("the target below it went as well: %+v", got)
	}
	if cfg.Alerts.CPU != 90 {
		t.Fatalf("the rest of the file went as well: alerts.cpu is %v", cfg.Alerts.CPU)
	}
}

// An item is addressed by its name, so reordering the file in an editor does
// not change which machine a save is about.
func TestSaveFindsATargetWhereverItSits(t *testing.T) {
	path := writeSaveFixture(t, `wake:
  - name: nas
    mac: 11:22:33:44:55:66
  - name: gaming-pc
    mac: AA:BB:CC:DD:EE:FF
`)
	saveOne(t, path, Patch{Wake: []WakePatch{{Name: "gaming-pc", MAC: "AA:BB:CC:DD:EE:00"}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if got := cfg.FindWakeTarget("gaming-pc"); got == nil || got.MAC != "AA:BB:CC:DD:EE:00" {
		t.Fatalf("the wrong item was edited: %+v", cfg.Wake)
	}
	if got := cfg.FindWakeTarget("nas"); got == nil || got.MAC != "11:22:33:44:55:66" {
		t.Fatalf("the first item changed: %+v", got)
	}
}

// A MAC that cannot be one is refused before the file is touched, in the words
// config validate uses for the same mistake.
func TestSaveRefusesAMACThatIsNotOne(t *testing.T) {
	path := writeSaveFixture(t, wakeFixture)
	before := readFile(t, path)

	rev, err := ReadRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	err = Save(path, rev, Patch{Wake: []WakePatch{{Name: "nas", MAC: "11:22:33:44:55"}}})
	if err == nil {
		t.Fatal("a five-octet address was accepted")
	}
	if !strings.Contains(err.Error(), "Expected format: AA:BB:CC:DD:EE:FF") {
		t.Fatalf("the refusal does not say what a MAC looks like: %v", err)
	}

	// config validate says the same thing about the same address. The two
	// differ only in the first letter: a Go error is lowercase because it gets
	// wrapped, and a validate finding is a sentence on its own.
	message, hint := macProblem("11:22:33:44:55")
	want := strings.ToLower(strings.TrimSuffix(message, ".") + " " + hint)
	if strings.ToLower(err.Error()) != want {
		t.Fatalf("the save and config validate disagree:\nsave:     %v\nvalidate: %s %s", err, message, hint)
	}

	if after := readFile(t, path); after != before {
		t.Fatal("the file was written anyway")
	}
}

// The editor is told a path and a value and nothing else, so a section becomes
// editable by describing it. These two are not wired to a patch yet — #154b2
// does that — and they are the shapes that will land on it, so the machine is
// held to them now rather than after it has been built around one list.

const serversFixture = `servers:
  - name: pi
    host: 192.168.0.4
    user: pi        # the one with the disk
  - name: nas
    host: 192.168.0.9
    user: admin

alerts:
  cpu: 90
`

func TestTheEditorHandlesTheServerList(t *testing.T) {
	edited, err := applyEdits([]byte(serversFixture),
		[]edit{
			{path: []step{mapKey("servers"), seqItem("nas"), mapKey("user")}, value: "root"},
			{path: []step{mapKey("servers"), seqItem("media"), mapKey("name")}, value: "media"},
			{path: []step{mapKey("servers"), seqItem("media"), mapKey("host")}, value: "192.168.0.20"},
		},
		[]removal{{path: []step{mapKey("servers"), seqItem("pi")}}},
	)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	cfg, err := parseConfigBytes(t, edited)
	if err != nil {
		t.Fatalf("the result does not load: %v\n%s", err, edited)
	}
	if cfg.FindServer("pi") != nil {
		t.Fatalf("pi was not removed:\n%s", edited)
	}
	if got := cfg.FindServer("nas"); got == nil || got.User != "root" {
		t.Fatalf("nas was not edited: %+v\n%s", got, edited)
	}
	if got := cfg.FindServer("media"); got == nil || got.Host != "192.168.0.20" {
		t.Fatalf("media was not added: %+v\n%s", got, edited)
	}
	if !strings.Contains(string(edited), "# the one with the disk") {
		// The comment belongs to the line pi's user is on, which went with it.
		t.Log("the removed item took its own comment, which is correct")
	}
}

const proxmoxFixture = `proxmox:
  - name: pve
    host: 192.168.0.50
    token_id: root@pam!homebutler
  - name: pve2
    host: 192.168.0.51
`

func TestTheEditorHandlesTheProxmoxList(t *testing.T) {
	edited, err := applyEdits([]byte(proxmoxFixture),
		[]edit{
			{path: []step{mapKey("proxmox"), seqItem("pve2"), mapKey("host")}, value: "192.168.0.52"},
			{path: []step{mapKey("proxmox"), seqItem("pve2"), mapKey("token_id")}, value: quoteIfNeeded("root@pam!homebutler")},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	cfg, err := parseConfigBytes(t, edited)
	if err != nil {
		t.Fatalf("the result does not load: %v\n%s", err, edited)
	}
	if len(cfg.Proxmox) != 2 {
		t.Fatalf("expected two endpoints, got %d:\n%s", len(cfg.Proxmox), edited)
	}
	second := cfg.Proxmox[1]
	if second.Host != "192.168.0.52" {
		t.Fatalf("the host did not change: %+v\n%s", second, edited)
	}
	// A token id holds @ and !, which have to survive being written and read.
	if second.TokenID != "root@pam!homebutler" {
		t.Fatalf("the token id did not round-trip: %q\n%s", second.TokenID, edited)
	}
	if cfg.Proxmox[0].TokenID != "root@pam!homebutler" {
		t.Fatalf("the first endpoint changed: %+v\n%s", cfg.Proxmox[0], edited)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func parseConfigBytes(t *testing.T, data []byte) (*Config, error) {
	t.Helper()
	path := writeSaveFixture(t, string(data))
	return Load(path)
}
