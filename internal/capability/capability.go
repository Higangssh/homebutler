// Package capability is what homebutler can do, described once.
//
// It was private to internal/mcp, which made the MCP tool list the only place
// the answer existed: the dashboard had eleven hand-registered endpoints and no
// way to know it was missing the other twenty-nine. Risk and targets are not
// protocol details — risk is what decides whether an operation needs a token
// and a confirmation, and targets is what decides which selector applies — so
// they belong to homebutler rather than to one of its interfaces.
package capability

import "sort"

// Definition is what a capability is called, what it does, and what it takes.
// The json tags are the MCP tools/list shape and must not drift: a client reads
// them to decide how to call the tool.
type Definition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema Schema `json:"inputSchema"`
}

type Schema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
}

type Risk string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
)

// TargetKind is what a tool can be pointed at. A bool could only ask "is this a
// named SSH server from servers:", which is the only kind of target homebutler
// has had so far. An API-backed target has no agent on the far side and a
// different way of being addressed, so the question has to be open-ended before
// 1.0 freezes the answer.
type TargetKind string

const (
	TargetLocal   TargetKind = "local"   // this machine; no target argument
	TargetServer  TargetKind = "server"  // a named SSH server from servers:
	TargetProxmox TargetKind = "proxmox" // a named endpoint from proxmox:
)

// HTTP is how a capability is reached from the dashboard, or why it is not.
//
// Every capability carries one, because the failure this field exists to
// prevent is silent: a tool added to the registry appears in MCP immediately
// and in the browser never, and nothing said which of those was intended. An
// absent exposure has to name its reason, and Exposed is what the server
// registers routes from rather than a second list kept alongside this one.
type HTTP struct {
	Method string // GET or POST; empty when the dashboard cannot reach it
	Path   string
	Absent string // why not, when Method is empty
	// Protection is what a caller has to bring. It is recorded rather than
	// inferred from Risk so that exposing something and deciding how it is
	// guarded are the same edit, and so a mismatch between the two is a test
	// failure rather than a judgement nobody wrote down.
	Protection Protection
}

// Protection is what reaching a capability from a browser costs.
type Protection string

// The tiers are decided by one question: can this be undone by doing something
// else? A restarted container comes back and a started guest can be shut down,
// so asking twice for those would buy nothing and teach people to click through
// confirmations — which is what the two tiers below it depend on not happening.
//
// All three are frozen at 1.0. docs/compatibility.md carries the table.
const (
	// Nothing beyond being able to reach the page.
	ProtectionNone Protection = ""
	// A bearer token, and the route is not registered at all without one.
	// Reads, settings, and actions that are undone by doing something else.
	ProtectionToken Protection = "token"
	// A token, plus a confirmation the caller sends deliberately — the same
	// line --confirm draws for a guest action in the CLI. For a destructive
	// action that is reversible: the service comes back when it is started
	// again.
	ProtectionTokenAndConfirm Protection = "token+confirm"
	// A token, plus the name of the target echoed back in the request. For a
	// destructive action that is not reversible — data is gone, and a click
	// cannot say which thing the operator meant to lose.
	ProtectionTokenAndName Protection = "token+name"
)

// protections is the closed set, so a capability cannot carry a tier written
// on the spot. See TestProtectionsAreFromTheClosedSet.
var protections = map[Protection]bool{
	ProtectionNone:            true,
	ProtectionToken:           true,
	ProtectionTokenAndConfirm: true,
	ProtectionTokenAndName:    true,
}

// Exposed reports whether the dashboard can reach this capability.
func (c Capability) Exposed() bool { return c.HTTP.Method != "" }

// The reasons a capability is not on the HTTP surface. A closed set: a reason
// written inline would be prose nobody could count, and this list going out of
// `GET /api/capabilities` makes each string a sentence we say to whoever asks
// what the dashboard can do.
//
// Every one of these says "not yet". None of them says "never" — that is a
// product decision, and when one is taken the reason moves here as its own
// constant rather than being implied by silence.
const (
	// A read nobody has built a screen for. Not a decision against it.
	AbsentNoViewYet = "no view built for it yet"
	// #154 gave the dashboard a write surface: it edits the config, with a
	// token, and the validation comes back from the one validator. What it did
	// not decide is running an action — a restart is not a setting, and the
	// confirmation a browser should ask for before starting one is open.
	AbsentNoActionRuleYet = "the dashboard writes settings since #154, and no rule has been decided for running an action from a browser"
	// The same question with data loss behind it, which is why it is a
	// different reason and a different issue.
	AbsentNoDestructiveRuleYet = "no confirmation rule for a destructive action in a browser: #242. The MCP tools take an explicit confirm argument; a browser tab holding a token is a weaker credential than a shell, and what it must show before it removes data has not been decided."
)

// absentReasons is the closed set, so a capability cannot carry a reason
// written on the spot. See TestAbsentReasonsAreFromTheClosedSet.
var absentReasons = map[string]bool{
	AbsentNoViewYet:            true,
	AbsentNoActionRuleYet:      true,
	AbsentNoDestructiveRuleYet: true,
}

type Capability struct {
	Tool    Definition
	Risk    Risk
	Targets []TargetKind
	HTTP    HTTP
}

// supports reports whether the tool can be pointed at kind.
func (c Capability) Supports(kind TargetKind) bool {
	for _, t := range c.Targets {
		if t == kind {
			return true
		}
	}
	return false
}

// For returns the registry entry for a tool name.
func For(name string) (Capability, bool) {
	for _, c := range Registry {
		if c.Tool.Name == name {
			return c, true
		}
	}
	return Capability{}, false
}

func Definitions() []Definition {
	defs := make([]Definition, 0, len(Registry))
	for _, c := range Registry {
		defs = append(defs, c.Tool)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

var Registry = []Capability{
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Method: "GET", Path: "/api/proxmox/status"},
		Tool: Definition{
			Name:        "proxmox_status",
			Description: "Get Proxmox VE version, cluster status, and resources",
			InputSchema: Schema{Type: "object", Properties: proxmoxEndpointProperties()},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "proxmox_guests",
			Description: "List Proxmox QEMU and LXC guests, optionally filtered by node, status, or type",
			InputSchema: Schema{Type: "object", Properties: map[string]Property{
				"endpoint": {Type: "string", Description: "Proxmox endpoint name from config (optional when exactly one is configured)"},
				"node":     {Type: "string", Description: "Only guests on this Proxmox node (optional)"},
				"status":   {Type: "string", Description: "Only guests with this status, such as running or stopped (optional)"},
				"type":     {Type: "string", Description: "Only guests of this type: qemu or lxc (optional)"},
			}},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "proxmox_node",
			Description: "Get detailed Proxmox node status",
			InputSchema: Schema{Type: "object", Properties: map[string]Property{
				"endpoint": {Type: "string", Description: "Proxmox endpoint name from config (optional when exactly one is configured)"},
				"node":     {Type: "string", Description: "Proxmox node name"},
			}, Required: []string{"node"}},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "proxmox_tasks",
			Description: "Get the 50 most recent Proxmox tasks for a node",
			InputSchema: Schema{Type: "object", Properties: map[string]Property{
				"endpoint": {Type: "string", Description: "Proxmox endpoint name from config (optional when exactly one is configured)"},
				"node":     {Type: "string", Description: "Proxmox node name"},
			}, Required: []string{"node"}},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/status"},
		Tool: Definition{
			Name:        "system_status",
			Description: "Get system status including CPU, memory, disk usage, and uptime, for the machine this binary runs on. Inside a container that is the container, not the host underneath it",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Method: "POST", Path: "/api/proxmox/guests/{vmid}/start", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "proxmox_guest_start",
			Description: "Start one explicitly targeted Proxmox guest after confirmation and return the accepted task UPID",
			InputSchema: proxmoxGuestActionSchema(),
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Method: "POST", Path: "/api/proxmox/guests/{vmid}/reboot", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "proxmox_guest_reboot",
			Description: "Reboot one explicitly targeted Proxmox guest after confirmation and return the accepted task UPID",
			InputSchema: proxmoxGuestActionSchema(),
		},
	},
	{
		Risk:    RiskDestructive,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Method: "POST", Path: "/api/proxmox/guests/{vmid}/shutdown", Protection: ProtectionTokenAndConfirm},
		Tool: Definition{
			Name:        "proxmox_guest_shutdown",
			Description: "Gracefully shut down one explicitly targeted Proxmox guest after confirmation and return the accepted task UPID",
			InputSchema: proxmoxGuestActionSchema(),
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetProxmox},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "proxmox_task_status",
			Description: "Inspect one asynchronous Proxmox task by node and opaque UPID",
			InputSchema: Schema{Type: "object", Properties: map[string]Property{
				"endpoint": {Type: "string", Description: "Explicit Proxmox endpoint name from config"},
				"node":     {Type: "string", Description: "Proxmox node name"},
				"upid":     {Type: "string", Description: "Opaque Proxmox task UPID"},
			}, Required: []string{"endpoint", "node", "upid"}},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "proxmox_script_list",
			Description: "List the curated Proxmox VE Community Scripts catalog (community-scripts/ProxmoxVE)",
			InputSchema: Schema{Type: "object"},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "proxmox_script_command",
			Description: "Render the pinned install command for one Proxmox VE Community Script. Never fetches or runs it; the caller reviews and runs it themselves on the Proxmox host",
			InputSchema: Schema{Type: "object", Properties: map[string]Property{
				"slug": {Type: "string", Description: "Script slug from proxmox_script_list, such as docker"},
			}, Required: []string{"slug"}},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/docker"},
		Tool: Definition{
			Name:        "docker_list",
			Description: "List Docker containers with their status, image, and ports. Stopped containers are included, so a name appearing here is not a name that is running — read state",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/docker/{name}/restart", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "docker_restart",
			Description: "Restart a Docker container by name. It goes down and comes back, and the result says the restart command succeeded, not that the app inside is serving again. Read the logs first if you do not know why it needs restarting",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"name":   {Type: "string", Description: "Container name to restart"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		Risk:    RiskDestructive,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/docker/{name}/stop", Protection: ProtectionTokenAndConfirm},
		Tool: Definition{
			Name:        "docker_stop",
			Description: "Stop a Docker container by name. Nothing here starts it again: there is no start tool, so the operator brings it back themselves",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"name":   {Type: "string", Description: "Container name to stop"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "docker_logs",
			Description: "Get logs from a Docker container: the last lines only, 50 by default, and it returns rather than following the stream",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"name":   {Type: "string", Description: "Container name to get logs from"},
					"lines":  {Type: "string", Description: "Number of log lines to return (default: 50)"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/docker/stats"},
		Tool: Definition{
			Name:        "docker_stats",
			Description: "Get resource usage statistics (CPU, memory, network, block I/O) for all running Docker containers",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "docker_top",
			Description: "List the processes running inside a Docker container, read from the host. Read-only: no exec, no TTY",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"name":   {Type: "string", Description: "Container name to inspect"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "docker_inspect",
			Description: "Summarize a Docker container's image, state, restart policy, ports, mounts, networks, and health. Environment variable values are never included",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"name":   {Type: "string", Description: "Container name to summarize"},
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"name"},
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Method: "POST", Path: "/api/wake/{name}", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "wake",
			Description: "Send a Wake-on-LAN magic packet to wake a machine. The packet is fire-and-forget: a successful result means it was sent, not that anything woke up",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"target":    {Type: "string", Description: "MAC address or configured device name"},
					"broadcast": {Type: "string", Description: "Broadcast address (default: 255.255.255.255)"},
				},
				Required: []string{"target"},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/ports"},
		Tool: Definition{
			Name:        "open_ports",
			Description: "List open network ports with associated process information. The process behind a port is not always readable without privilege, and missing_process says so rather than leaving the field quietly empty",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "network_scan",
			Description: "Scan the local network to discover devices (IP, MAC, hostname). It probes every address on the subnet and takes up to 30 seconds, so it is an answer to a question somebody asked rather than a way to begin",
			InputSchema: Schema{
				Type: "object",
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/alerts"},
		Tool: Definition{
			Name:        "alerts",
			Description: "Check resource alerts for CPU, memory, and disk usage against configured thresholds. It reads and compares; nothing is sent anywhere and nothing is recorded, which is what a running watcher does instead",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "inventory_scan",
			Description: "Collect server inventory/topology including system status, Docker containers, app ports, and system ports",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "inventory_export",
			Description: "Export server inventory/topology as a Mermaid diagram locally, or JSON locally/remotely",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"format": {Type: "string", Description: "Export format: mermaid (default, local) or json"},
					"server": {Type: "string", Description: "Remote server name from config (optional; remote supports format=json)"},
				},
			},
		},
	},
	{
		// Reading the comparison and saving a snapshot are separate over HTTP:
		// GET /api/report compares without saving, so a dashboard polling it
		// cannot prune the baseline somebody wanted. This entry is the write —
		// the one that saves.
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/report/snapshot", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "report",
			Description: "Generate a butler-style health report with snapshot comparison, warnings, notable changes, and suggested actions. It saves a snapshot unless no_save is set, which moves the window every later comparison is measured from — so a loop that calls this leaves nothing to compare against",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"keep":    {Type: "number", Description: "Number of snapshots to retain (default: 30)"},
					"no_save": {Type: "boolean", Description: "Preview without writing a snapshot"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/doctor"},
		Tool: Definition{
			Name:        "doctor",
			Description: "Run a read-only diagnosis for resource pressure, stopped containers, public ports, backup hygiene, notifications, and report baseline readiness",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"backup_max_age_hours": {Type: "number", Description: "Warn when the latest backup is older than this many hours (default: 168)"},
					"server":               {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		// Write rather than read: CheckTargets records the new container state
		// and saves any incident it detects, so a caller cannot treat this as a
		// free query the way system_status is.
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/watch/check", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "watch_check",
			Description: "Run a one-shot restart check on watched targets and report restarts detected since the last check. Only docker targets can be inspected this way; systemd and pm2 targets are reported as skipped rather than assumed healthy",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/processes"},
		Tool: Definition{
			Name:        "processes",
			Description: "List the top processes by CPU or memory, with a total count and any zombies broken out separately",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"limit":   {Type: "string", Description: "Number of processes to return (default: 10, 0 for all)"},
					"sort_by": {Type: "string", Description: "Sort by cpu (default) or mem"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		// Local only. It answers "is the config this MCP server is running on
		// valid", which is a question about this machine. Pointing it at a
		// remote would silently answer about a different file.
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "config_validate",
			Description: "Check the config file this server is running on: which file was used, which rule selected it, what was read from each section, and anything wrong or silently ignored",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"strict": {Type: "boolean", Description: "Treat warnings as failures in the passed field (default: false)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/watch/incidents"},
		Tool: Definition{
			Name:        "watch_history",
			Description: "List recorded restart incidents, newest first. Captured logs are excluded unless include_logs is set, because every incident carries a hundred lines of output twice over",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"limit":        {Type: "string", Description: "Most recent N incidents (default: 10, 0 for all)"},
					"container":    {Type: "string", Description: "Only incidents for this target (optional)"},
					"include_logs": {Type: "boolean", Description: "Include the logs captured before and after each restart (default: false)"},
					"server":       {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "GET", Path: "/api/watch"},
		Tool: Definition{
			Name:        "watch_list",
			Description: "List the targets being watched, with their kind and what the last check recorded",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		// Putting a target on the watch list writes one file under the watch
		// directory. No privilege is taken and nothing is installed, which is
		// what separates it from watch_install (#157).
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/watch/targets", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "watch_add",
			Description: "Add a Docker container, systemd unit, or PM2 app to the watch list. This writes the list and nothing begins watching it — a supervisor has to be installed separately, which only the operator can do. Adding a target twice is reported as added=false rather than an error",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"container": {Type: "string", Description: "Container, unit, or app name to watch"},
					"kind":      {Type: "string", Description: "What it is: docker, systemd, or pm2 (default docker)", Enum: []string{"docker", "systemd", "pm2"}},
					"unit":      {Type: "string", Description: "Actual unit or app name when it differs from the name above (optional)"},
					"server":    {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"container"},
			},
		},
	},
	{
		// Removing a target stops future checks. Recorded incidents are left
		// alone, so nothing already observed is lost.
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/watch/targets/{name}/remove", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "watch_remove",
			Description: "Remove a target from the watch list, leaving its recorded incidents in place. Only the list changes: whatever was supervising it keeps running until the operator stops it",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"container": {Type: "string", Description: "Name to stop watching"},
					"server":    {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"container"},
			},
		},
	},
	{
		// Sends one real message through every configured channel, which is a
		// write in the sense that matters: something leaves the machine and
		// arrives on someone's phone.
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/notify/test", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "notify_test",
			Description: "Send one test notification through every configured channel and report which ones arrived. A real message goes out to each, so anyone reading those channels sees it",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "alerts_history",
			Description: "Show recorded alert and remediation history. Entries are only written while a watcher is running, so an empty list means nothing was recording rather than nothing went wrong",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/backup", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "backup_create",
			Description: "Create a Docker compose backup archive for all services or one service. Volumes are read while the containers run, so a database mid-write can land inconsistent; an archive is not evidence it restores, which is what backup_drill answers",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"service": {Type: "string", Description: "Specific service to back up (optional)"},
					"to":      {Type: "string", Description: "Custom backup destination directory (optional)"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "backup_list",
			Description: "List existing backup archives in the configured backup directory. It reads names, sizes and dates — that an archive is here says nothing about whether it restores, which is what backup_drill answers",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"server": {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/backup/drill", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "backup_drill",
			Description: "Verify a backup by booting an app in an isolated Docker environment and checking that it responds. A second copy runs beside the live one on a network and port of its own, and everything it made is removed either way. A pass means the archive is not corrupt and the app starts on it, not that every row is there",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"app":     {Type: "string", Description: "App/service to drill (required unless all=true)"},
					"all":     {Type: "boolean", Description: "Drill all supported apps in the backup"},
					"archive": {Type: "string", Description: "Specific backup archive to verify (optional)"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
			},
		},
	},
	{
		Risk:    RiskDestructive,
		Targets: []TargetKind{TargetLocal, TargetServer},
		HTTP:    HTTP{Method: "POST", Path: "/api/backup/restore", Protection: ProtectionTokenAndName},
		Tool: Definition{
			Name:        "backup_restore",
			Description: "Restore Docker volumes from a backup archive, overwriting the data the app is running on. Destructive: confirm intent before calling. Bind mounts declared by the archive are always refused here, because an agent has no way to name a host path it may write to",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"archive": {Type: "string", Description: "Backup archive path to restore"},
					"service": {Type: "string", Description: "Specific service to restore (optional)"},
					"server":  {Type: "string", Description: "Remote server name from config (optional, runs locally if omitted)"},
				},
				Required: []string{"archive"},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "install_list",
			Description: "List available self-hosted apps that can be installed. The catalogue is compiled into the binary, so this reaches no network and answers the same on any machine",
			InputSchema: Schema{
				Type: "object",
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Method: "POST", Path: "/api/install/{app}", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "install_app",
			Description: "Install a self-hosted app via docker compose. Pre-checks docker, ports, and duplicates automatically, and a refusal comes back as a result with the reasons rather than as an error",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"app":  {Type: "string", Description: "App name (e.g. uptime-kuma, vaultwarden)"},
					"port": {Type: "string", Description: "Custom host port (optional, uses default if omitted)"},
				},
				Required: []string{"app"},
			},
		},
	},
	{
		Risk:    RiskRead,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Absent: AbsentNoViewYet},
		Tool: Definition{
			Name:        "install_status",
			Description: "Check the status of an installed app, as its containers report it. An app homebutler did not install is not known here",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"app": {Type: "string", Description: "App name"},
				},
				Required: []string{"app"},
			},
		},
	},
	{
		Risk:    RiskWrite,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Method: "POST", Path: "/api/install/{app}/uninstall", Protection: ProtectionToken},
		Tool: Definition{
			Name:        "install_uninstall",
			Description: "Stop an installed app and remove its containers. The app directory and its volumes stay on disk; install_purge is the one that deletes them",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"app": {Type: "string", Description: "App name"},
				},
				Required: []string{"app"},
			},
		},
	},
	{
		Risk:    RiskDestructive,
		Targets: []TargetKind{TargetLocal},
		HTTP:    HTTP{Method: "POST", Path: "/api/install/{app}/purge", Protection: ProtectionTokenAndName},
		Tool: Definition{
			Name:        "install_purge",
			Description: "Stop an installed app and delete all data including containers, config, and volumes. Nothing here restores it and no backup is taken first: take one before calling if the data matters",
			InputSchema: Schema{
				Type: "object",
				Properties: map[string]Property{
					"app": {Type: "string", Description: "App name"},
				},
				Required: []string{"app"},
			},
		},
	},
}

func proxmoxEndpointProperties() map[string]Property {
	return map[string]Property{
		"endpoint": {Type: "string", Description: "Proxmox endpoint name from config (optional when exactly one is configured)"},
	}
}

func proxmoxGuestActionSchema() Schema {
	return Schema{Type: "object", Properties: map[string]Property{
		"endpoint": {Type: "string", Description: "Explicit Proxmox endpoint name from config"},
		"node":     {Type: "string", Description: "Proxmox node name"},
		"type":     {Type: "string", Description: "Guest type: qemu or lxc"},
		"vmid":     {Type: "integer", Description: "Proxmox guest VMID from 1 through 999999999"},
		"confirm":  {Type: "boolean", Description: "Must be true to confirm the explicit guest action target"},
	}, Required: []string{"endpoint", "node", "type", "vmid", "confirm"}}
}
