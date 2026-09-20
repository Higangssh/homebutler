package mcp

import (
	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/backup"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/install"
	"github.com/Higangssh/homebutler/internal/inventory"
	"github.com/Higangssh/homebutler/internal/network"
	"github.com/Higangssh/homebutler/internal/notify"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/proxmox"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/wake"
	"github.com/Higangssh/homebutler/internal/watch"
)

// Output says what a tool answers with.
//
// The registry already freezes how a tool is called — its name, its arguments,
// what calling it may do. It said nothing about what comes back, and what
// comes back is the part an agent branches on. Eight tools answered with a map
// written at the call site, which is a shape with no name, and one of them
// answered with two different shapes depending on how it went.
//
// Exactly one of Frozen and Passthrough is set. Frozen is a zero value of the
// type the tool returns, and those are what the contract golden records.
// Passthrough names whose shape it is when it is not ours: freezing a Proxmox
// response would be promising Proxmox's field names, which we cannot keep.
type Output struct {
	Frozen      any
	Passthrough string
}

func frozen(v any) Output           { return Output{Frozen: v} }
func passthrough(who string) Output { return Output{Passthrough: who} }

// ToolOutputs declares the answer shape of every tool in the registry. A test
// fails when a tool is missing from here, so adding one without deciding what
// it answers with is not possible — getting the decision wrong still is, and
// docs/compatibility.md says so.
var ToolOutputs = map[string]Output{
	// Reads of the machine, all shapes we wrote.
	"system_status":  frozen(system.StatusInfo{}),
	"processes":      frozen(system.ProcessResult{}),
	"open_ports":     frozen(ports.Result{}),
	"network_scan":   frozen([]network.Device{}),
	"inventory_scan": frozen(inventory.Inventory{}),

	// Docker. Curated projections rather than Docker's own output — Inspect
	// returns name, image, status, ports and mounts, not `docker inspect`.
	"docker_list":    frozen([]docker.Container{}),
	"docker_stats":   frozen([]docker.ContainerStats{}),
	"docker_logs":    frozen(docker.LogsResult{}),
	"docker_top":     frozen(docker.TopResult{}),
	"docker_inspect": frozen(docker.InspectResult{}),
	"docker_restart": frozen(docker.ActionResult{}),
	"docker_stop":    frozen(docker.ActionResult{}),

	"wake":           frozen(wake.WakeResult{}),
	"alerts":         frozen(alerts.AlertResult{}),
	"alerts_history": frozen([]alerts.HistoryEntry{}),
	"notify_test":    frozen([]notify.TestResult{}),

	"report": frozen(report.Report{}),
	"doctor": frozen(doctor.Result{}),

	"watch_check":   frozen(watch.RunResult{}),
	"watch_history": frozen([]watch.Incident{}),
	"watch_list":    frozen([]watch.WatchedTarget{}),
	"watch_add":     frozen(WatchAddResult{}),
	"watch_remove":  frozen(WatchRemoveResult{}),

	"backup_create":  frozen(backup.BackupResult{}),
	"backup_list":    frozen([]backup.ListEntry{}),
	"backup_drill":   frozen(backup.DrillResult{}),
	"backup_restore": frozen(backup.RestoreResult{}),

	"install_list":      frozen([]install.App{}),
	"install_app":       frozen(InstallResult{}),
	"install_status":    frozen(InstallStatusResult{}),
	"install_uninstall": frozen(InstallResult{}),
	"install_purge":     frozen(InstallResult{}),

	"config_validate":  frozen(ConfigValidateResult{}),
	"inventory_export": frozen(InventoryExportResult{}),

	// Proxmox. The catalogue and the action result are ours; the reads are
	// decoded from the Proxmox API into the same struct they are emitted from,
	// so their field names are Proxmox's.
	"proxmox_script_list":    frozen([]proxmox.Script{}),
	"proxmox_script_command": frozen(ProxmoxScriptCommandResult{}),
	"proxmox_guest_start":    frozen(proxmoxGuestActionResult{}),
	"proxmox_guest_reboot":   frozen(proxmoxGuestActionResult{}),
	"proxmox_guest_shutdown": frozen(proxmoxGuestActionResult{}),

	"proxmox_status":      passthrough("the Proxmox API, inside an envelope of ours"),
	"proxmox_guests":      passthrough("the Proxmox API"),
	"proxmox_node":        passthrough("the Proxmox API"),
	"proxmox_tasks":       passthrough("the Proxmox API"),
	"proxmox_task_status": passthrough("the Proxmox API"),
}
