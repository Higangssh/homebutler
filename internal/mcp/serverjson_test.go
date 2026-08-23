package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serverManifest mirrors the fields of server.json that the MCP Registry
// validates. The publish step rewrites the two version fields from the tag,
// so those are checked for shape rather than for a particular value.
type serverManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Repository  struct {
		URL string `json:"url"`
	} `json:"repository"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Version      string `json:"version"`
	} `json:"packages"`
}

// npmManifest is the part of npm/package.json the registry reads. It proves
// ownership of the npm package by matching mcpName against the server name.
type npmManifest struct {
	MCPName string `json:"mcpName"`
}

func loadNPMManifest(t *testing.T) npmManifest {
	t.Helper()
	path := filepath.Join("..", "..", "npm", "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var m npmManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return m
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

// The registry grants publish rights to io.github.<login>/* from the GitHub
// OIDC token, and the login keeps its original casing. v0.21.1 declared
// io.github.higangssh/homebutler against a grant for io.github.Higangssh/*
// and was rejected with a 403, after the binaries and the npm package had
// already been published. The owner in repository.url is the same login, so
// the expected namespace can be derived rather than repeated.
func TestServerJSONNamespaceMatchesRepositoryOwner(t *testing.T) {
	m := loadServerManifest(t)

	const prefix = "https://github.com/"
	if !strings.HasPrefix(m.Repository.URL, prefix) {
		t.Fatalf("repository.url %q is not a github.com URL", m.Repository.URL)
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(m.Repository.URL, prefix), "/"), "/")
	if len(parts) != 2 {
		t.Fatalf("cannot read owner and repo from repository.url %q", m.Repository.URL)
	}

	want := "io.github." + parts[0] + "/" + parts[1]
	if m.Name != want {
		t.Errorf("server.json name is %q, but repository.url implies %q.\n"+
			"The registry matches this against the OIDC grant io.github.<login>/*, case included.",
			m.Name, want)
	}
}

// The registry confirms ownership of the npm package by reading mcpName from
// the published package and matching it against the server name. Correcting
// one without the other trades a 403 for a different rejection at the same
// point in the release.
func TestNPMMCPNameMatchesServerName(t *testing.T) {
	server := loadServerManifest(t)
	npm := loadNPMManifest(t)

	if npm.MCPName != server.Name {
		t.Errorf("npm/package.json mcpName is %q, server.json name is %q; the registry requires both to agree",
			npm.MCPName, server.Name)
	}
}
