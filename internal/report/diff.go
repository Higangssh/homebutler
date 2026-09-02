package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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
)

// Nouns a collapsed line counts, one per collector.
const (
	nounContainers = "containers"
	nounPorts      = "ports"
	nounMounts     = "mounts"
)

var kindRank = map[string]int{
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
			detail := shortID(p.ID) + " → " + shortID(c.ID)
			if p.Image != c.Image {
				detail += ", " + p.Image + " → " + c.Image
			}
			changes = append(changes, Change{Kind: kindReplaced, Subject: name, Detail: detail, noun: nounContainers})
		case p.Image != c.Image:
			changes = append(changes, Change{Kind: kindImage, Subject: name, Detail: p.Image + " → " + c.Image, noun: nounContainers})
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
			changes = append(changes, Change{Kind: kindGone, Subject: key, Detail: describeOwner("was ", p), noun: nounPorts})
			continue
		}
		if owner(p) != owner(c) && owner(p) != "" && owner(c) != "" {
			changes = append(changes, Change{Kind: kindPort, Subject: key, Detail: owner(p) + " → " + owner(c), noun: nounPorts})
		}
	}
	for key, c := range currByPort {
		if _, ok := prevByPort[key]; !ok {
			changes = append(changes, Change{Kind: kindNew, Subject: key, Detail: describeOwner("now ", c), noun: nounPorts})
		}
	}
	return changes
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
		if len(c.Kind) > kindWidth {
			kindWidth = len(c.Kind)
		}
		if len(c.Subject) > subjectWidth {
			subjectWidth = len(c.Subject)
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

// indexPorts keys a listener by protocol and port number, which is the pair
// that identifies it. The protocol is omitted from the key's display form
// when the platform tool did not report one, rather than printing a trailing
// slash with nothing after it.
func indexPorts(list []ports.PortInfo) map[string]ports.PortInfo {
	out := make(map[string]ports.PortInfo, len(list))
	for _, p := range list {
		key := ":" + p.Port
		if p.Protocol != "" {
			key += "/" + p.Protocol
		}
		out[key] = p
	}
	return out
}

// owner names the process behind a listener. An empty result means the
// platform tool did not report one — usually a listener owned by another
// user — and the renderer leaves the detail column blank rather than
// claiming the owner changed to "unknown".
func owner(p ports.PortInfo) string {
	return p.Process
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

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
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
