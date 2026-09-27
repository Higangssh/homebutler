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

// ToolOutputs declares what every tool in the registry answers with, as a zero
// value of the type it returns. The contract golden records those types field
// by field.
//
// The registry already freezes how a tool is called — its name, its arguments,
// what calling it may do. It said nothing about what comes back, and what
// comes back is the part an agent branches on. Eight tools answered with a map
// written at the call site, which is a shape with no name, and one of them
// answered with two different shapes depending on how it went.
//
// A test fails when a tool is missing from here, so adding one without
// deciding what it answers with is not possible — getting the decision wrong
// still is, and docs/compatibility.md says so.
var ToolOutputs = map[string]any{
	// Reads of the machine, all shapes we wrote.
	"system_status":  system.StatusDoc{},
	"processes":      system.ProcessResult{},
	"open_ports":     ports.Result{},
	"network_scan":   []network.Device{},
	"inventory_scan": inventory.Inventory{},

	// Docker. Curated projections rather than Docker's own output — Inspect
	// returns name, image, status, ports and mounts, not `docker inspect`.
	"docker_list":    []docker.Container{},
	"docker_stats":   []docker.ContainerStats{},
	"docker_logs":    docker.LogsResult{},
	"docker_top":     docker.TopResult{},
	"docker_inspect": docker.InspectResult{},
	"docker_restart": docker.ActionResult{},
	"docker_stop":    docker.ActionResult{},

	"wake":           wake.WakeResult{},
	"alerts":         alerts.AlertResult{},
	"alerts_history": []alerts.HistoryEntry{},
	"notify_test":    []notify.TestResult{},

	"report": report.Report{},
	"doctor": doctor.Result{},

	"watch_check":   watch.RunResult{},
	"watch_history": []watch.Incident{},
	"watch_list":    []watch.WatchedTarget{},
	"watch_add":     WatchAddResult{},
	"watch_remove":  WatchRemoveResult{},

	"backup_create":  backup.BackupResult{},
	"backup_list":    []backup.ListEntry{},
	"backup_drill":   backup.DrillResult{},
	"backup_restore": backup.RestoreResult{},

	"install_list":      []install.App{},
	"install_app":       InstallResult{},
	"install_status":    InstallStatusResult{},
	"install_uninstall": InstallResult{},
	"install_purge":     InstallResult{},

	"config_validate":  ConfigValidateResult{},
	"inventory_export": InventoryExportResult{},

	// Proxmox. The catalogue and the action result are ours.
	"proxmox_script_list":    []proxmox.Script{},
	"proxmox_script_command": ProxmoxScriptCommandResult{},
	"proxmox_guest_start":    proxmox.GuestActionResult{},
	"proxmox_guest_reboot":   proxmox.GuestActionResult{},
	"proxmox_guest_shutdown": proxmox.GuestActionResult{},

	// So are the reads, although they come from the Proxmox API. Every one
	// is decoded into a struct of ours and written under that struct's keys,
	// so the key set is ours even where a name is Proxmox's (pveversion,
	// exitstatus). Node, Guest and Store rename on the way through: maxcpu
	// becomes max_cpu, storage becomes name, tags becomes a list. The five
	// were recorded as passthrough until 0.40.0, which left outside the
	// freeze the shape the guest actions take their vmid, node and type from.
	"proxmox_status":      proxmox.DefaultView{},
	"proxmox_guests":      []proxmox.Guest{},
	"proxmox_node":        proxmox.NodeStatus{},
	"proxmox_tasks":       []proxmox.Task{},
	"proxmox_task_status": proxmox.TaskStatus{},
}
