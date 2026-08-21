package inventory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
)

// RenderTree returns a human-readable tree view of the inventory.
func RenderTree(inv *Inventory) string {
	var b strings.Builder
	links := dockerPortLinks(inv.Containers)
	displayPorts := dedupeDisplayPorts(inv.Ports, links)
	appPorts, systemPorts := splitPorts(displayPorts, links)
	summary := inventorySummary(inv, appPorts, systemPorts)

	b.WriteString("🏠 Home Network\n")
	fmt.Fprintf(&b, "   Server  %s (%s)\n", inv.ServerName, inv.Host)
	fmt.Fprintf(&b, "   Summary %s\n", summary)

	if inv.System != nil {
		b.WriteString("\n📊 System\n")
		fmt.Fprintf(&b, "   └─ %s/%s · CPU %.0f%% · Mem %.1f/%.1fGB · Up %s\n",
			inv.System.OS, inv.System.Arch,
			inv.System.CPU.UsagePercent,
			inv.System.Memory.UsedGB, inv.System.Memory.TotalGB,
			inv.System.Uptime)
	}

	if len(inv.Containers) > 0 {
		fmt.Fprintf(&b, "\n📦 Containers (%d)\n", len(inv.Containers))
		for i, c := range inv.Containers {
			lastContainer := i == len(inv.Containers)-1
			fmt.Fprintf(&b, "   %s %s %s · %s\n", branch(lastContainer), containerIcon(c.State), c.Name, friendlyContainerState(c.State))

			details := []string{"image " + c.Image}
			if mappings := dockerPortMappings(c.Ports); len(mappings) > 0 {
				details = append(details, "exposes "+strings.Join(mappings, ", "))
			}
			for j, detail := range details {
				fmt.Fprintf(&b, "   %s%s %s\n", childPrefix(lastContainer), branch(j == len(details)-1), detail)
			}
		}
	}

	if len(appPorts) > 0 {
		fmt.Fprintf(&b, "\n🌐 App Ports (%d)\n", len(appPorts))
		for i, p := range appPorts {
			fmt.Fprintf(&b, "   %s %s :%s/%s · %s\n", branch(i == len(appPorts)-1), exposureIcon(p), p.Port, p.Protocol, friendlyPortOwner(p, links[p.Port]))
		}
	}

	if len(systemPorts) > 0 {
		fmt.Fprintf(&b, "\n🧩 System Ports (%d)\n", len(systemPorts))
		for i, p := range systemPorts {
			fmt.Fprintf(&b, "   %s %s :%s/%s · %s\n", branch(i == len(systemPorts)-1), exposureIcon(p), p.Port, p.Protocol, friendlyProcess(p.Process))
		}
	}

	if len(inv.Warnings) > 0 {
		fmt.Fprintf(&b, "\n⚠️  Warnings (%d)\n", len(inv.Warnings))
		for i, w := range inv.Warnings {
			fmt.Fprintf(&b, "   %s %s\n", branch(i == len(inv.Warnings)-1), w)
		}
	}

	return b.String()
}

// RenderTreeFiltered renders a filtered view of the inventory.
// Supported filters: "exposed" (only ports bound to public interfaces).
func RenderTreeFiltered(inv *Inventory, filter string) (string, error) {
	switch filter {
	case "exposed":
		return renderExposedPorts(inv), nil
	default:
		return "", fmt.Errorf("unsupported filter %q (supported: %s)", filter, strings.Join(SupportedFilters(), ", "))
	}
}

// SupportedFilters returns the filter values accepted by RenderTreeFiltered.
func SupportedFilters() []string {
	return []string{"exposed"}
}

// IsSupportedFilter reports whether filter is accepted by RenderTreeFiltered.
func IsSupportedFilter(filter string) bool {
	for _, f := range SupportedFilters() {
		if f == filter {
			return true
		}
	}
	return false
}

// renderExposedPorts renders a compact view of the ports bound to public interfaces.
func renderExposedPorts(inv *Inventory) string {
	var b strings.Builder
	links := dockerPortLinks(inv.Containers)
	displayPorts := dedupeDisplayPorts(inv.Ports, links)
	exposed := exposedPorts(displayPorts)

	b.WriteString("🏠 Home Network\n")
	fmt.Fprintf(&b, "   Server  %s\n", inv.ServerName)

	b.WriteString("\n🌐 Exposed Ports\n")
	if len(exposed) == 0 {
		b.WriteString("   (none)\n")
		return b.String()
	}
	for i, p := range exposed {
		fmt.Fprintf(&b, "   %s :%s/%s · %s\n", branch(i == len(exposed)-1), p.Port, p.Protocol, friendlyPortOwner(p, links[p.Port]))
	}
	return b.String()
}

// exposedPorts returns only ports bound to public interfaces, preserving order.
func exposedPorts(all []ports.PortInfo) []ports.PortInfo {
	var out []ports.PortInfo
	for _, p := range all {
		if isPublicBind(p.Address) {
			out = append(out, p)
		}
	}
	return out
}

// RenderMermaid returns a Mermaid graph TD diagram of the inventory.
func RenderMermaid(inv *Inventory) string {
	var b strings.Builder
	b.WriteString("graph TD\n")

	serverID := sanitizeID(inv.ServerName)
	serverLabel := mermaidLabel("🖥 " + inv.ServerName + "<br/>" + inv.Host)
	links := dockerPortLinks(inv.Containers)
	displayPorts := dedupeDisplayPorts(inv.Ports, links)
	containerIDs := make(map[string]string)

	fmt.Fprintf(&b, "  home[\"🏠 Home Network\"] --> %s[\"%s\"]\n", serverID, serverLabel)

	for i, c := range inv.Containers {
		cID := fmt.Sprintf("c%d", i)
		containerIDs[c.Name] = cID
		label := mermaidLabel(fmt.Sprintf("📦 %s<br/>%s", c.Name, friendlyContainerState(c.State)))
		fmt.Fprintf(&b, "  %s --> %s[\"%s\"]\n", serverID, cID, label)
	}

	for i, p := range displayPorts {
		pID := fmt.Sprintf("p%d", i)
		owner := friendlyProcess(p.Process)
		if containers := links[p.Port]; len(containers) > 0 {
			owner = strings.Join(containers, ", ")
			if isForwarderProcess(p.Process) {
				owner += "<br/>forwarded by " + friendlyProcess(p.Process)
			}
		}
		label := mermaidLabel(fmt.Sprintf("%s :%s/%s<br/>%s", exposureIcon(p), p.Port, p.Protocol, owner))
		fmt.Fprintf(&b, "  %s --> %s[\"%s\"]\n", serverID, pID, label)
		for _, container := range links[p.Port] {
			if cID := containerIDs[container]; cID != "" {
				fmt.Fprintf(&b, "  %s -. exposes .-> %s\n", cID, pID)
			}
		}
	}

	return b.String()
}

func branch(last bool) string {
	if last {
		return "└─"
	}
	return "├─"
}

func childPrefix(parentLast bool) string {
	if parentLast {
		return "   "
	}
	return "│  "
}

func inventorySummary(inv *Inventory, appPorts, systemPorts []ports.PortInfo) string {
	running, stopped := 0, 0
	for _, c := range inv.Containers {
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}
	public, local := 0, 0
	for _, p := range append(append([]ports.PortInfo{}, appPorts...), systemPorts...) {
		if ports.IsPublicBind(p.Address) {
			public++
		} else {
			local++
		}
	}
	parts := []string{
		fmt.Sprintf("✅ %d running", running),
		fmt.Sprintf("⚪ %d stopped", stopped),
		fmt.Sprintf("🌍 %d public ports", public),
		fmt.Sprintf("🔒 %d local ports", local),
	}
	return strings.Join(parts, " · ")
}

func dedupeDisplayPorts(all []ports.PortInfo, links map[string][]string) []ports.PortInfo {
	seen := make(map[string]bool)
	out := make([]ports.PortInfo, 0, len(all))
	for _, p := range all {
		key := displayPortKey(p, links[p.Port])
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func displayPortKey(p ports.PortInfo, containers []string) string {
	owner := friendlyProcess(p.Process)
	if len(containers) > 0 {
		owner = strings.Join(containers, ",")
	}
	return strings.Join([]string{p.Protocol, p.Port, exposureIcon(p), owner}, "|")
}

func splitPorts(all []ports.PortInfo, links map[string][]string) (appPorts, systemPorts []ports.PortInfo) {
	for _, p := range all {
		if len(links[p.Port]) > 0 {
			appPorts = append(appPorts, p)
		} else {
			systemPorts = append(systemPorts, p)
		}
	}
	return appPorts, systemPorts
}

func friendlyPortOwner(p ports.PortInfo, containers []string) string {
	if len(containers) == 0 {
		return friendlyProcess(p.Process)
	}
	owner := strings.Join(containers, ", ")
	if isForwarderProcess(p.Process) {
		return owner + " (forwarded by " + friendlyProcess(p.Process) + ")"
	}
	return owner
}

func friendlyContainerState(state string) string {
	switch state {
	case "running":
		return "running"
	case "created":
		return "not started"
	case "exited":
		return "stopped"
	case "restarting":
		return "restarting"
	default:
		if state == "" {
			return "unknown"
		}
		return state
	}
}

func containerIcon(state string) string {
	switch state {
	case "running":
		return "✅"
	case "restarting":
		return "⚠️"
	default:
		return "⚪"
	}
}

func friendlyProcess(process string) string {
	if process == "" {
		return "unknown"
	}
	if process == "limactl" {
		return "Colima/Lima"
	}
	return process
}

func isForwarderProcess(process string) bool {
	return process == "ssh" || process == "limactl"
}

func exposureIcon(p ports.PortInfo) string {
	if ports.IsPublicBind(p.Address) {
		return "🌍"
	}
	return "🔒"
}

var mappedPortRe = regexp.MustCompile(`(?:^|[\s,])(?:[\d.:\[\]]+:)?(\d+)->(\d+)/(tcp|udp)`)

func dockerPortLinks(containers []docker.Container) map[string][]string {
	links := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for _, c := range containers {
		for _, match := range mappedPortRe.FindAllStringSubmatch(c.Ports, -1) {
			if len(match) < 2 || match[1] == "" {
				continue
			}
			port := match[1]
			if seen[port] == nil {
				seen[port] = make(map[string]bool)
			}
			if seen[port][c.Name] {
				continue
			}
			seen[port][c.Name] = true
			links[port] = append(links[port], c.Name)
		}
	}
	for port := range links {
		sort.Strings(links[port])
	}
	return links
}

func dockerPortMappings(raw string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, match := range mappedPortRe.FindAllStringSubmatch(raw, -1) {
		if len(match) < 4 {
			continue
		}
		mapping := fmt.Sprintf(":%s → %s/%s", match[1], match[2], match[3])
		if seen[mapping] {
			continue
		}
		seen[mapping] = true
		out = append(out, mapping)
	}
	return out
}

func mermaidLabel(s string) string {
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "\n", "<br/>")
	return s
}

// sanitizeID makes a string safe for use as a Mermaid node ID.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "server"
	}
	return b.String()
}
