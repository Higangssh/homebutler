package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// serverManifest mirrors the fields of server.json that the MCP Registry
// validates. The publish step rewrites the two version fields from the tag,
// so those are checked for shape rather than for a particular value.
type serverManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Packages    []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Version      string `json:"version"`
	} `json:"packages"`
}

func loadServerManifest(t *testing.T) serverManifest {
	t.Helper()
	path := filepath.Join("..", "..", "server.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var m serverManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return m
}

// The registry rejects a description over 100 characters with a 422, and that
// happens in the last step of the release, after the binaries, the Homebrew
// tap and the npm package have already been published. v0.21.0 shipped with a
// 103-character description and never reached the registry. Fail here instead.
//
// The limit is not ours: it is ServerDetail.description.maxLength in the
// schema server.json already names in its $schema field, generated from the
// registry's own openapi.yaml. Hardcoded rather than fetched so the test does
// not need the network; check the schema if a future release is rejected for
// a field this does not cover.
func TestServerJSONDescriptionWithinRegistryLimit(t *testing.T) {
	const maxDescription = 100

	m := loadServerManifest(t)
	if got := len(m.Description); got > maxDescription {
		t.Errorf("server.json description is %d characters, registry rejects over %d:\n%s",
			got, maxDescription, m.Description)
	}
}

// The registry rejects a server version whose package version does not exist
// on npm, so the two have to move together. The publish step sets both from
// the tag; this catches a hand-edit that touches only one.
func TestServerJSONVersionsAgree(t *testing.T) {
	m := loadServerManifest(t)
	if len(m.Packages) == 0 {
		t.Fatal("server.json declares no packages")
	}
	if m.Version != m.Packages[0].Version {
		t.Errorf("server.json version %q does not match packages[0].version %q",
			m.Version, m.Packages[0].Version)
	}
}
