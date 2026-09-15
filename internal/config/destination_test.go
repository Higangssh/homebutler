package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// A saved password is sent to whatever answers at the address in the file, and
// the address is a one-field edit. These tests are the reason the rule lives
// in Save rather than in the form: every caller has to pass it, including the
// ones that do not exist yet.

const withCredentials = `servers:
  - name: nas
    host: 192.168.0.9
    user: admin
    auth: password
    password: hunter2
  - name: pi
    host: 192.168.0.4
    user: pi

proxmox:
  - name: pve
    host: 192.168.0.50
    token_id: root@pam!homebutler
    token: tok_read
`

func str(s string) *string { return &s }

func saveExpectingRefusal(t *testing.T, path string, patch Patch) error {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := ReadRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	err = Save(path, rev, patch)
	if err == nil {
		t.Fatal("the save was accepted")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Fatal("the file was written anyway")
	}
	return err
}

func TestMovingAServerWithoutItsPasswordIsRefused(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	err := saveExpectingRefusal(t, path, Patch{Servers: []ServerPatch{{Name: "nas", Host: str("10.0.0.6")}}})

	if !errors.Is(err, ErrCredentialWouldMove) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "Send the password again") {
		t.Fatalf("the refusal does not say what to do: %v", err)
	}
}

// The account being signed into is part of the destination: the same password
// offered as a different user is still the password, offered.
func TestMovingAServerToAnotherAccountIsRefused(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	if err := saveExpectingRefusal(t, path, Patch{Servers: []ServerPatch{{Name: "nas", User: str("root")}}}); !errors.Is(err, ErrCredentialWouldMove) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// Somebody who knows the password can move the server. Somebody who only has
// the config cannot take the password with them.
func TestMovingAServerWithItsPasswordIsAllowed(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	saveOne(t, path, Patch{Servers: []ServerPatch{{
		Name:     "nas",
		Host:     str("10.0.0.6"),
		Password: SetSecret("a-new-one"),
	}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	server := cfg.FindServer("nas")
	if server == nil || server.Host != "10.0.0.6" || server.Password != "a-new-one" {
		t.Fatalf("the change did not land: %+v", server)
	}
}

// Clearing it is just as good: there is no longer a credential to carry.
func TestMovingAServerAfterClearingItsPasswordIsAllowed(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	saveOne(t, path, Patch{Servers: []ServerPatch{{
		Name:     "nas",
		Host:     str("10.0.0.6"),
		AuthMode: str("key"),
		Password: ClearSecret(),
	}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if server := cfg.FindServer("nas"); server == nil || server.Password != "" || server.Host != "10.0.0.6" {
		t.Fatalf("the change did not land: %+v", server)
	}
}

// A server with no saved password has nothing to lose by moving. The key never
// leaves this machine — what that does not protect is recorded in #154b2.
func TestMovingAServerThatSignsInWithAKeyIsAllowed(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	saveOne(t, path, Patch{Servers: []ServerPatch{{Name: "pi", Host: str("192.168.0.44")}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if server := cfg.FindServer("pi"); server == nil || server.Host != "192.168.0.44" {
		t.Fatalf("the change did not land: %+v", server)
	}
}

// A password left behind on a server that now signs in with a key still counts.
// It is in the file, and switching the mode back is another one-field edit.
func TestAPasswordLeftOnAKeyServerStillCounts(t *testing.T) {
	path := writeSaveFixture(t, `servers:
  - name: nas
    host: 192.168.0.9
    auth: key
    password: left-behind
`)
	if err := saveExpectingRefusal(t, path, Patch{Servers: []ServerPatch{{Name: "nas", Host: str("10.0.0.6")}}}); !errors.Is(err, ErrCredentialWouldMove) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

func TestMovingAProxmoxEndpointWithoutItsTokenIsRefused(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	err := saveExpectingRefusal(t, path, Patch{Proxmox: []ProxmoxPatch{{Name: "pve", Host: str("10.0.0.50")}}})

	if !errors.Is(err, ErrCredentialWouldMove) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("the refusal does not name the credential: %v", err)
	}
}

func TestMovingAProxmoxEndpointWithItsTokenIsAllowed(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	saveOne(t, path, Patch{Proxmox: []ProxmoxPatch{{
		Name:  "pve",
		Host:  str("10.0.0.50"),
		Token: SetSecret("tok_new"),
	}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if len(cfg.Proxmox) != 1 || cfg.Proxmox[0].Host != "10.0.0.50" || cfg.Proxmox[0].Token != "tok_new" {
		t.Fatalf("the change did not land: %+v", cfg.Proxmox)
	}
}

// A credential kept in a file cannot be sent again from here, so the address
// cannot be changed from here either. Saying that is better than moving it.
func TestMovingAnEndpointWhoseTokenIsInAFileIsRefused(t *testing.T) {
	path := writeSaveFixture(t, `proxmox:
  - name: pve
    host: 192.168.0.50
    token_id: root@pam!homebutler
    token_file: /etc/homebutler/pve.token
`)
	err := saveExpectingRefusal(t, path, Patch{Proxmox: []ProxmoxPatch{{Name: "pve", Host: str("10.0.0.50")}}})
	if !errors.Is(err, ErrCredentialWouldMove) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "Edit the config file instead") {
		t.Fatalf("the refusal does not say where the change can be made: %v", err)
	}
}

// Everything else about an item can still be changed without re-sending
// anything: the rule is about the address, not about editing.
func TestChangingSomethingOtherThanTheAddressIsAllowed(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	saveOne(t, path, Patch{Servers: []ServerPatch{{Name: "nas", Rename: "storage"}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if cfg.FindServer("storage") == nil || cfg.FindServer("nas") != nil {
		t.Fatalf("the rename did not land: %+v", cfg.Servers)
	}
	if server := cfg.FindServer("storage"); server.Password != "hunter2" || server.Host != "192.168.0.9" {
		t.Fatalf("the rename changed something else: %+v", server)
	}
}

// A server being added carries whatever credential it is given, so there is
// nothing to move.
func TestAddingAServerWithAPasswordIsAllowed(t *testing.T) {
	path := writeSaveFixture(t, withCredentials)
	saveOne(t, path, Patch{Servers: []ServerPatch{{
		Name:     "media",
		Host:     str("192.168.0.20"),
		User:     str("media"),
		AuthMode: str("password"),
		Password: SetSecret("brand-new"),
	}}})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	server := cfg.FindServer("media")
	if server == nil || server.Host != "192.168.0.20" || server.Password != "brand-new" {
		t.Fatalf("the server was not added: %+v", cfg.Servers)
	}
	if other := cfg.FindServer("nas"); other == nil || other.Password != "hunter2" {
		t.Fatalf("adding one server disturbed another: %+v", other)
	}
}
