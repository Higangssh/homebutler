package mcp

import (
	"github.com/Higangssh/homebutler/internal/config"
)

// The shapes a tool returns when there is no type elsewhere to return.
//
// These were `map[string]any` literals written at the call site. A map has no
// name, so nothing could record what a tool answers with, nothing could tell a
// renamed key from a new one, and two branches of the same tool could answer
// with different keys — which is what `install_app` did. A caller reads these
// and branches on them, so they are shapes whether or not they have names.

// ConfigValidateResult is what `config_validate` answers with. The verdict
// travels with the counts rather than the verdict being an error, so a caller
// that gates on Passed still gets to see why.
//
// Errors and Warnings are counts, not lists — the detail is in Result.
type ConfigValidateResult struct {
	Passed   bool                     `json:"passed"`
	Errors   int                      `json:"errors"`
	Warnings int                      `json:"warnings"`
	Result   *config.ValidationResult `json:"result"`
}

// WatchAddResult is what `watch_add` answers with. Added is false when the
// target was already watched, which is not a failure.
type WatchAddResult struct {
	Container string `json:"container"`
	Kind      string `json:"kind"`
	Added     bool   `json:"added"`
}

// WatchRemoveResult is what `watch_remove` answers with.
type WatchRemoveResult struct {
	Container string `json:"container"`
	Removed   bool   `json:"removed"`
}

// InstallResult is what the three install actions answer with.
//
// One shape for all three, because `install_app` used to answer with
// `{status, issues}` when it refused and `{status, app, port, path, state}`
// when it worked — two shapes from one tool, and an agent that had only seen
// one of them had no `app` to read. Every outcome now names the app it is
// about, and the fields that only apply to some outcomes are omitted rather
// than invented.
type InstallResult struct {
	// Status is installed, failed, uninstalled or purged.
	Status string `json:"status"`
	App    string `json:"app"`
	// Issues is why a refusal was a refusal. Present only on failed.
	Issues []string `json:"issues,omitempty"`
	Port   string   `json:"port,omitempty"`
	Path   string   `json:"path,omitempty"`
	State  string   `json:"state,omitempty"`
	// DataPreserved distinguishes uninstall from purge, and is absent for the
	// outcomes where the question does not arise.
	DataPreserved *bool `json:"data_preserved,omitempty"`
}

// InstallStatusResult is what `install_status` answers with. It is a read and
// has no outcome, which is why it does not carry Status.
type InstallStatusResult struct {
	App   string `json:"app"`
	State string `json:"state"`
}

// ProxmoxScriptCommandResult is what `proxmox_script_command` answers with.
// The warning travels with the command because homebutler prints these for a
// person to review and run, and never runs one itself.
type ProxmoxScriptCommandResult struct {
	Slug    string `json:"slug"`
	Command string `json:"command"`
	Warning string `json:"warning"`
}

// InventoryExportResult is what `inventory_export` answers with when the
// format is mermaid. Asking for json returns the inventory itself.
type InventoryExportResult struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}
