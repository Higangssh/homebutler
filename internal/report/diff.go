package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/ports"
	"github.com/Higangssh/homebutler/internal/style"
	"github.com/Higangssh/homebutler/internal/system"
)

// Change is one identity-level difference between two snapshots.
//
// Kind, Subject and Detail are kept apart rather than pre-formatted into a
// sentence so that grouping and column alignment stay rendering decisions.
// NotableChanges carries the complete, ungrouped list for --json; only the
// human renderer collapses and truncates it, because a person needs the
// section to stay readable and an agent needs all of it.
type Change struct {
	Kind    string
	Subject string
	Detail  string

	// noun is what a collapsed line counts. It comes from the collector the
	// change was found in, not from Kind: "gone" describes both a container
	// and a listener, and a line reading "11 containers" over a list of
	// ports is worse than not collapsing at all.
	noun string
}

// Change kinds, ordered by how much an operator can act on them.
const (
	kindGone     = "gone"
	kindNew      = "new"
	kindReplaced = "replaced"
	kindImage    = "image"
	kindState    = "state"
	kindPort     = "port"
	kindDisk     = "disk"
	kindSkipped  = "skipped"
)

// Nouns a collapsed line counts, one per collector.
const (
	nounContainers = "containers"
	nounPorts      = "ports"
	nounMounts     = "mounts"
	nounProcesses  = "processes"
)

var kindRank = map[string]int{
	// A skipped collector qualifies everything under it, so it goes first.
	kindSkipped:  -1,
	kindGone:     0,
	kindNew:      1,
	kindReplaced: 2,
	kindImage:    3,
	kindState:    4,
	kindPort:     5,
	kindDisk:     6,
}

// groupThreshold is the number of changes of one kind above which the human
// renderer collapses them into a single line. Thirty containers coming back
// after a reboot is one fact, not thirty.
const groupThreshold = 5

// groupNamed is how many subjects a collapsed line names before counting the
// rest. Enough to recognise what happened, short enough to stay on one line.
const groupNamed = 3

func (c Change) String() string {
	if c.Detail == "" {
		return c.Kind + ": " + c.Subject
	}
	return c.Kind + ": " + c.Subject + " — " + c.Detail
}

// diffContainers compares two container lists by name, then by identity.
//
// Matching on name is what makes a swap visible: a list of six before and six
// after is unchanged by count, and a different container behind the same name
// is exactly the event the report exists to surface.
func diffContainers(prev, curr []docker.Container) []Change {
	prevByName := indexContainers(prev)
	currByName := indexContainers(curr)

	var changes []Change
	for name, p := range prevByName {
		c, ok := currByName[name]
		if !ok {
			changes = append(changes, Change{Kind: kindGone, Subject: name, Detail: "was " + p.State, noun: nounContainers})
			continue
		}
		switch {
		case p.ID != c.ID:
			// The detail column is prose a person reads; the ID pair is the
			// evidence, not the sentence. "recreated" is what docker compose
			// up -d does to a container, which is how most people meet this.
			detail := "recreated, " + shortID(p.ID) + " → " + shortID(c.ID)
			if p.Image != c.Image {
				detail += ", " + p.Image + " → " + c.Image
			}
			changes = append(changes, Change{Kind: kindReplaced, Subject: name, Detail: detail + stateSuffix(p, c), noun: nounContainers})
		case p.Image != c.Image:
			changes = append(changes, Change{Kind: kindImage, Subject: name, Detail: p.Image + " → " + c.Image + stateSuffix(p, c), noun: nounContainers})
		case p.State != c.State:
			changes = append(changes, Change{Kind: kindState, Subject: name, Detail: p.State + " → " + c.State, noun: nounContainers})
		}
	}
	for name, c := range currByName {
		if _, ok := prevByName[name]; !ok {
			changes = append(changes, Change{Kind: kindNew, Subject: name, Detail: "now " + c.State, noun: nounContainers})
		}
	}
	return changes
}

// diffPorts compares two listener lists by protocol and port number.
//
// A port that changes owner keeps the count identical, which is the same
// blind spot the container diff above closes.
func diffPorts(prev, curr []ports.PortInfo) []Change {
	prevByPort := indexPorts(prev)
	currByPort := indexPorts(curr)

	var changes []Change
	for key, p := range prevByPort {
		c, ok := currByPort[key]
		if !ok {
			changes = append(changes, Change{Kind: kindGone, Subject: portSubject(p), Detail: describeOwner("was ", p), noun: nounPorts})
			continue
		}
		if owner(p) != owner(c) && owner(p) != "" && owner(c) != "" {
			changes = append(changes, Change{Kind: kindPort, Subject: portSubject(p), Detail: owner(p) + " → " + owner(c), noun: nounPorts})
		}
	}
	for key, c := range currByPort {
		if _, ok := prevByPort[key]; !ok {
			changes = append(changes, Change{Kind: kindNew, Subject: portSubject(c), Detail: describeOwner("now ", c), noun: nounPorts})
		}
	}
	return changes
}

// dedupeChanges drops changes that render identically.
//
// Docker publishes a port on both address families, so one container starting
// produces 0.0.0.0:8099 and [::]:8099 — two listeners by identity, one line
// by display, and the reader gets the same sentence twice with nothing to
// tell them apart. One port opening is one event. This is not the grouping
// rule: grouping collapses different changes into a summary and is left to
// the human renderer, while an exact duplicate carries no information for
// anything reading the output, JSON included.
func dedupeChanges(changes []Change) []Change {
	seen := make(map[Change]bool, len(changes))
	out := changes[:0]
	for _, c := range changes {
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// sortChanges orders by how actionable the kind is, then by subject, so the
// same two snapshots always produce the same report. Map iteration above is
// unordered and would otherwise reshuffle the section on every run.
func sortChanges(changes []Change) {
	sort.SliceStable(changes, func(i, j int) bool {
		if kindRank[changes[i].Kind] != kindRank[changes[j].Kind] {
			return kindRank[changes[i].Kind] < kindRank[changes[j].Kind]
		}
		return changes[i].Subject < changes[j].Subject
	})
}

// groupChanges collapses runs of the same kind once there are more of them
// than a person will read one at a time.
func groupChanges(changes []Change) []Change {
	groups := map[string][]Change{}
	var order []string
	for _, c := range changes {
		key := c.Kind + "\x00" + c.noun
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c)
	}

	var out []Change
	for _, key := range order {
		group := groups[key]
		if len(group) <= groupThreshold {
			out = append(out, group...)
			continue
		}
		named := make([]string, 0, groupNamed)
		for _, c := range group[:groupNamed] {
			named = append(named, c.Subject)
		}
		out = append(out, Change{
			Kind:    group[0].Kind,
			Subject: strconv.Itoa(len(group)) + " " + group[0].noun,
			Detail:  strings.Join(named, ", ") + ", +" + strconv.Itoa(len(group)-groupNamed),
			noun:    group[0].noun,
		})
	}
	return out
}

// changeBlock renders changes as three aligned columns. Columns are padded
// here rather than left to the caller because the kind column is what the
// eye sorts on before it reads anything.
func changeBlock(changes []Change, indent string) string {
	kindWidth, subjectWidth := 0, 0
	for _, c := range changes {
		if w := lipgloss.Width(c.Kind); w > kindWidth {
			kindWidth = w
		}
		if w := lipgloss.Width(c.Subject); w > subjectWidth {
			subjectWidth = w
		}
	}

	var b strings.Builder
	for _, c := range changes {
		b.WriteString(indent)
		b.WriteString(renderKind(c.Kind, pad(c.Kind, kindWidth)))
		b.WriteString("  ")
		b.WriteString(style.Title.Render(pad(c.Subject, subjectWidth)))
		if c.Detail != "" {
			b.WriteString("  ")
			b.WriteString(style.Label.Render(c.Detail))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderKind colours by severity rather than by category: gone, new, and
// changed-in-place. Three colours, so the section sorts itself before it is
// read.
func renderKind(kind, text string) string {
	switch kind {
	case kindGone:
		return style.Fail.Render(text)
	case kindNew:
		return style.OK.Render(text)
	case kindSkipped:
		return style.Dim.Render(text)
	default:
		return style.Warn.Render(text)
	}
}

func indexContainers(list []docker.Container) map[string]docker.Container {
	out := make(map[string]docker.Container, len(list))
	for _, c := range list {
		out[c.Name] = c
	}
	return out
}

// indexPorts keys a listener by the three things that identify it: bind
// address, port number and protocol.
//
// The address is load-bearing. Dropping it collapses 127.0.0.53:53 and
// 0.0.0.0:53 into one entry, so a reordering of ss output invents an owner
// change on a host where nothing moved — and it hides the change that matters
// most, a service that was on loopback and is now on every interface.
func indexPorts(list []ports.PortInfo) map[string]ports.PortInfo {
	out := make(map[string]ports.PortInfo, len(list))
	for _, p := range list {
		out[p.Address+"|"+p.Port+"|"+p.Protocol] = p
	}
	return out
}

// portSubject is the display form. A wildcard bind is written ":8080/tcp",
// since the address carries nothing a reader needs; anything else names the
// address, because that is the difference being reported.
func portSubject(p ports.PortInfo) string {
	subject := ":" + p.Port
	if p.Address != "" && !ports.IsPublicBind(p.Address) {
		subject = p.Address + ":" + p.Port
	}
	if p.Protocol != "" {
		subject += "/" + p.Protocol
	}
	return subject
}

// owner names the process behind a listener. An empty result means the
// platform tool did not report one — usually a listener owned by another
// user — and the renderer leaves the detail column blank rather than
// claiming the owner changed to "unknown".
func owner(p ports.PortInfo) string {
	return p.Process
}

// stateSuffix names a state transition that happened alongside a replacement
// or an image change. The switch above is exclusive, so without this a
// redeploy whose new container crashed reports only that it was replaced —
// which is the half of the story an operator does not need.
func stateSuffix(prev, curr docker.Container) string {
	if prev.State == curr.State {
		return ""
	}
	return ", " + prev.State + " → " + curr.State
}

// describeOwner prefixes a listener's owner, or returns nothing when the
// platform tool did not report one.
func describeOwner(prefix string, p ports.PortInfo) string {
	if owner(p) == "" {
		return ""
	}
	return prefix + owner(p)
}

// shortID trims a container ID to the prefix docker itself displays.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// pad measures display width rather than bytes, so a subject containing
// non-ASCII — a mount path, most often — does not push the detail column out
// of line. style.LabelledBlock measures the same way.
func pad(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// diffDisks reports a mount whose usage moved by more than half a gigabyte.
// Disk is the one resource number that earns a line: it moves in one
// direction, it does not recover on its own, and it is the reading that
// stops a server rather than slowing it.
func diffDisks(prev, curr []system.DiskInfo) []Change {
	var changes []Change
	for _, d := range curr {
		for _, pd := range prev {
			if d.Mount != pd.Mount {
				continue
			}
			delta := d.UsedGB - pd.UsedGB
			if delta > 0.5 || delta < -0.5 {
				changes = append(changes, Change{
					Kind:    kindDisk,
					Subject: d.Mount,
					Detail:  fmt.Sprintf("%+.1f GB since last report", delta),
					noun:    nounMounts,
				})
			}
			// One row per mount. Without this the loop is a cross product,
			// and a mount that appears twice in df output reports four times.
			break
		}
	}
	return changes
}

// collectorAnswered reports whether a collector produced data in both
// snapshots. Diffing against a snapshot taken while docker was down would
// report every container as gone, which is what the Failed field exists to
// prevent.
func collectorAnswered(a, b *Snapshot, collector string) bool {
	return !collectorFailed(a, collector) && !collectorFailed(b, collector)
}

func collectorFailed(s *Snapshot, collector string) bool {
	for _, f := range s.Failed {
		if f == collector {
			return true
		}
	}
	return false
}

// ProcessIdentity is what a snapshot keeps about a process: enough to tell
// two runs apart, and nothing that moves on every sample.
//
// CPU, memory and PID are absent by construction rather than by a rule in the
// differ. They change between any two runs, and a pid is recycled, so
// persisting them would invite a diff that reports noise. Hash is the first
// twelve hex characters of sha256 over the full command line: it separates
// two processes sharing an executable name without writing their arguments,
// which carry secrets in flags, into a file on disk.
type ProcessIdentity struct {
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
}

// minProcessAge is how long a process has to have been running before it is
// worth recording. Measured rather than guessed: two runs thirty seconds
// apart on an idle machine reported "new head" and "new sed" — the shell
// pipeline that was reading the report. Cron jobs, package managers and
// anything else that lives for a moment would arrive the same way.
//
// A process held back here is not lost. It is simply reported on the next
// run, by which time it has earned the word "new".
const minProcessAge = time.Minute

// processIdentities reduces a process list to the set of distinct identities.
//
// Duplicates are collapsed on purpose: eight workers sharing one invocation
// are one thing running, and a pool growing or shrinking is a resource
// signal rather than something appearing or disappearing.
func processIdentities(procs []system.ProcessInfo) []ProcessIdentity {
	seen := map[ProcessIdentity]bool{}
	var out []ProcessIdentity
	for _, p := range procs {
		// An unknown age is kept. Dropping it would make a running daemon
		// vanish from this snapshot and return in the next one, a pair of
		// changes for something that never stopped.
		if p.Elapsed >= 0 && p.Elapsed < minProcessAge {
			continue
		}
		id := ProcessIdentity{Name: p.Name}
		if p.Command != "" {
			sum := sha256.Sum256([]byte(p.Command))
			id.Hash = hex.EncodeToString(sum[:])[:12]
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Hash < out[j].Hash
	})
	return out
}

// diffProcesses compares two runs by executable name, then by invocation.
//
// "replaced" is reserved for the case where it is true: one invocation under a
// name before, one after, and they differ. The container analogy stops there —
// a container name maps to one container, while python3, bash, node and ssh
// routinely map to several, and calling it a replacement when one of four
// python3 invocations exits would describe something that did not happen.
// Those are reported as what they are, an arrival and a departure under a
// name that is still running.
func diffProcesses(prev, curr []ProcessIdentity) []Change {
	prevByName := groupProcessesByName(prev)
	currByName := groupProcessesByName(curr)

	var changes []Change
	for name, prevHashes := range prevByName {
		currHashes, ok := currByName[name]
		if !ok {
			changes = append(changes, Change{Kind: kindGone, Subject: name, noun: nounProcesses})
			continue
		}
		if sameHashes(prevHashes, currHashes) {
			continue
		}
		if len(prevHashes) == 1 && len(currHashes) == 1 {
			changes = append(changes, Change{
				Kind:    kindReplaced,
				Subject: name,
				Detail:  "same name, different invocation",
				noun:    nounProcesses,
			})
			continue
		}
		if departed := countMissing(prevHashes, currHashes); departed > 0 {
			changes = append(changes, Change{
				Kind:    kindGone,
				Subject: name,
				Detail:  invocationCount(departed) + ", others still running",
				noun:    nounProcesses,
			})
		}
		if arrived := countMissing(currHashes, prevHashes); arrived > 0 {
			changes = append(changes, Change{
				Kind:    kindNew,
				Subject: name,
				Detail:  invocationCount(arrived) + " under a name already running",
				noun:    nounProcesses,
			})
		}
	}
	for name := range currByName {
		if _, ok := prevByName[name]; !ok {
			changes = append(changes, Change{Kind: kindNew, Subject: name, noun: nounProcesses})
		}
	}
	return changes
}

// countMissing counts entries of a that b does not have.
func countMissing(a, b map[string]bool) int {
	var n int
	for k := range a {
		if !b[k] {
			n++
		}
	}
	return n
}

func invocationCount(n int) string {
	if n == 1 {
		return "1 invocation"
	}
	return strconv.Itoa(n) + " invocations"
}

func groupProcessesByName(list []ProcessIdentity) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, p := range list {
		if out[p.Name] == nil {
			out[p.Name] = map[string]bool{}
		}
		out[p.Name][p.Hash] = true
	}
	return out
}

func sameHashes(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// attentionFromChanges derives what to act on from what moved.
//
// The threshold checks above it answer "is something wrong right now"; these
// answer "did something happen that you would want to know about". Both are
// computed from the snapshots rather than from the rendered change strings —
// parsing our own prose to decide what matters would make the detail column
// load-bearing, which is exactly what the column rule says it must not be.
func attentionFromChanges(prev, snap *Snapshot) []string {
	var out []string

	for _, p := range newlyPublicListeners(prev.Ports, snap.Ports) {
		who := owner(p)
		if who == "" {
			who = "an unidentified process"
		}
		out = append(out, fmt.Sprintf(
			"Port %s is now reachable from every interface, answered by %s — it was not before",
			":"+p.Port+"/"+p.Protocol, who))
	}

	prevByName := indexContainers(prev.Containers)
	for _, c := range snap.Containers {
		p, existed := prevByName[c.Name]
		if !existed || c.State == "running" {
			continue
		}
		switch {
		case p.ID != c.ID:
			out = append(out, fmt.Sprintf(
				"%s was recreated and is %s, not running — the deploy did not come back", c.Name, c.State))
		case p.Image != c.Image:
			out = append(out, fmt.Sprintf(
				"%s took a new image and is %s, not running", c.Name, c.State))
		case p.State == "running":
			out = append(out, fmt.Sprintf(
				"%s stopped since the last report", c.Name))
		}
	}

	sort.Strings(out)
	return out
}

// newlyPublicListeners reports listeners that are reachable from every
// interface now and were not before.
//
// The comparison is per port and protocol rather than per listener, because
// the move that matters — 127.0.0.1:8080 becoming 0.0.0.0:8080 — is a
// departure and an arrival in the change list with nothing correlating them.
func newlyPublicListeners(prev, curr []ports.PortInfo) []ports.PortInfo {
	wasPublic := map[string]bool{}
	for _, p := range prev {
		if ports.IsPublicBind(p.Address) {
			wasPublic[p.Port+"/"+p.Protocol] = true
		}
	}

	seen := map[string]bool{}
	var out []ports.PortInfo
	for _, p := range curr {
		key := p.Port + "/" + p.Protocol
		if !ports.IsPublicBind(p.Address) || wasPublic[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}
