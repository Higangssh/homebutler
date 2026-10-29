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

// line is the change as --json carries it: the parts a caller branches on,
// plus the sentence String produces for a terminal.
func (c Change) line() ChangeLine {
	return ChangeLine{
		Kind:   c.Kind,
		Target: c.Subject,
		Detail: c.Detail,
		Text:   c.String(),
	}
}

// Kinds is every word that can appear as a change's kind. The README
// documents these eight and promises an agent can branch on them; a ninth
// added without a line in that table would be a promise quietly broken, which
// is what TestKindsMatchTheReadme watches for.
func Kinds() []string {
	return []string{kindGone, kindNew, kindReplaced, kindImage, kindState, kindPort, kindDisk, kindSkipped}
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
		if ownerIdentity(p) != ownerIdentity(c) && owner(p) != "" && owner(c) != "" {
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
	if p.Process != "" {
		return p.Process
	}
	// On Linux an ordinary user is told nothing about a port published by
	// Docker, because root's docker-proxy holds it. The container that
	// published it was collected in the same run.
	return p.Container
}

// ownerIdentity is what has to differ for a port to have changed hands: the
// process, or for a port Docker published, the container's name. owner() is
// "name (image)" for a container, and comparing that called a port taken over
// when the same container had only had its image tag move — the 0.41.2 soak
// reported "answered by it-tools now, and was not at the last report" about
// the container that had answered it all along. The image change is the
// image kind's to report, and a recreated container is replaced's.
func ownerIdentity(p ports.PortInfo) string {
	if p.Process != "" {
		return p.Process
	}
	name, _, _ := strings.Cut(p.Container, " (")
	return name
}

// unidentified is what to say when neither the operating system nor the
// container list could name what is answering. It names the one thing that
// would answer it rather than leaving a reader with nowhere to go.
const unidentified = "an unidentified process (sudo ss -tlnp names it)"

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

	// Young says the process had been running for less than minProcessAge
	// when the snapshot was taken, and what was known about it then:
	// youngUnconfirmed or youngRestarted. Empty is a process that had been
	// running for longer, which is every process in a snapshot written
	// before this field existed: those releases kept only old ones.
	Young string `json:"young,omitempty"`

	// age is how long the oldest process sharing this identity had been
	// running, on this run. It is never written; it is what tells a process
	// that started again since the last report from one that has not.
	age time.Duration
}

// What a young process in a snapshot was known to be.
const (
	// youngUnconfirmed was there when the processes were first compared, and
	// has not yet been seen running for a minute. It staying is not new, and
	// it leaving is not gone: until a snapshot has seen it a minute old,
	// nothing says it was more than passing through. A shell, a pipeline or
	// a job that runs for a few seconds every so often stays here for good,
	// and never gets a line.
	youngUnconfirmed = "unconfirmed"
	// youngRestarted had been seen running for a minute, and was under a
	// minute old again. Once is a restart, which is not a change; doing it
	// again before the next report is.
	youngRestarted = "restarted"
)

// key is the identity without what this run knew about its age, which is
// what two snapshots are matched on.
func (p ProcessIdentity) key() ProcessIdentity {
	return ProcessIdentity{Name: p.Name, Hash: p.Hash}
}

// minProcessAge is how long a process has to have been running before a
// report will say it started or stopped. Measured rather than guessed: two runs thirty
// seconds apart on an idle machine reported "new head" and "new sed" — the
// shell pipeline that was reading the report. Cron jobs, package managers and
// anything else that lives for a moment would arrive the same way.
//
// The rule is about what a comparison may say, not about what a snapshot
// keeps. It used to be both, and a baseline taken half a minute after a
// container was recreated left that container's processes out, so the next
// report called them new though they had been running all along. A young
// process is now kept, marked young; see settleProcesses for what a snapshot
// that is not a baseline does with one.
const minProcessAge = time.Minute

// isYoung reports a process that has been running for less than
// minProcessAge. An unknown age is not young: holding it back would make a
// running daemon vanish from one comparison and return in the next, and on a
// ps whose elapsed time cannot be read it would be every process.
func isYoung(p system.ProcessInfo) bool {
	return p.Elapsed >= 0 && p.Elapsed < minProcessAge
}

// processIdentities reduces a process list to the set of distinct identities.
//
// Duplicates are collapsed on purpose: eight workers sharing one invocation
// are one thing running, and a pool growing or shrinking is a resource
// signal rather than something appearing or disappearing. An identity is
// young only when every process sharing it is: one old worker is enough to
// say the invocation was already running. A young identity starts out
// unconfirmed, which is what a baseline keeps.
func processIdentities(procs []system.ProcessInfo) []ProcessIdentity {
	byKey := map[ProcessIdentity]*ProcessIdentity{}
	var order []ProcessIdentity
	for _, p := range procs {
		key := identityOf(p)
		id, seen := byKey[key]
		if !seen {
			order = append(order, key)
			id = &ProcessIdentity{Name: key.Name, Hash: key.Hash, Young: youngUnconfirmed}
			byKey[key] = id
		}
		if !isYoung(p) {
			id.Young = ""
		}
		if p.Elapsed > id.age {
			id.age = p.Elapsed
		}
	}
	out := make([]ProcessIdentity, 0, len(order))
	for _, key := range order {
		id := *byKey[key]
		if id.Young == "" {
			id.age = 0
		}
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

// settleProcesses is what a snapshot keeps of this run's processes once they
// have been compared against prev, the older snapshot's.
//
// A young process the older snapshot did not have is left out, as it always
// was: it started since the last report, and the next report says so if it is
// still running a minute later. Keeping it as unconfirmed would hide that,
// because an unconfirmed process becoming old is what a baseline's young
// processes do, and that is not news. One the older snapshot had is kept: it
// stays unconfirmed if that is all it was, and is a restart if it had been
// seen running for a minute.
//
// A baseline does not come through here, and keeps every young process as
// unconfirmed: nothing before it can say whether they are new.
func settleProcesses(curr, prev []ProcessIdentity) []ProcessIdentity {
	before := make(map[ProcessIdentity]ProcessIdentity, len(prev))
	for _, p := range prev {
		before[p.key()] = p
	}
	out := make([]ProcessIdentity, 0, len(curr))
	for _, c := range curr {
		if c.Young == "" {
			out = append(out, c)
			continue
		}
		p, existed := before[c.key()]
		if !existed {
			continue
		}
		if p.Young == youngUnconfirmed {
			c.Young = youngUnconfirmed
		} else {
			c.Young = youngRestarted
		}
		out = append(out, c)
	}
	return out
}

// identityOf is what a snapshot keeps of one process.
func identityOf(p system.ProcessInfo) ProcessIdentity {
	id := ProcessIdentity{Name: p.Name}
	if p.Command != "" {
		sum := sha256.Sum256([]byte(p.Command))
		id.Hash = hex.EncodeToString(sum[:])[:12]
	}
	return id
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
//
// A young identity is left out when the other side does not have it: young
// now and absent before is held back until it has run for a minute, and
// unconfirmed before and absent now was never seen running long enough to
// have been a change. A restarted one that is absent now is gone.
//
// since is how long before this run's process list the previous one was
// read. A process that had restarted just before the last report and has
// started again since is reported as restarting, which is the one thing a
// crash loop shows the process table: otherwise every restart keeps its
// place and nothing is ever said.
func diffProcesses(prev, curr []ProcessIdentity, since time.Duration) []Change {
	prevByName := groupProcessesByName(withoutUnmatched(prev, curr, youngUnconfirmed))
	currByName := groupProcessesByName(withoutUnmatched(curr, prev, ""))

	changes := restarting(prev, curr, since)
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

// withoutUnmatched returns list without the young identities other does not
// have: those marked only, or any young one when only is empty.
func withoutUnmatched(list, other []ProcessIdentity, only string) []ProcessIdentity {
	has := make(map[ProcessIdentity]bool, len(other))
	for _, o := range other {
		has[o.key()] = true
	}
	out := make([]ProcessIdentity, 0, len(list))
	for _, id := range list {
		held := id.Young != "" && (only == "" || id.Young == only)
		if held && !has[id.key()] {
			continue
		}
		out = append(out, id)
	}
	return out
}

// etimeSlack is how much younger than the time between two process lists a
// process has to be to have started after the first. ps prints whole
// seconds, and the clock is read just before ps runs, so a process that
// started a moment before the last list could otherwise look as if it had
// started after it, and one restart would be counted twice.
const etimeSlack = 2 * time.Second

// restarting reports the processes that have restarted at least twice: under
// a minute old at the last report, after having been seen running for
// longer, and started again since. A process that restarts once with the
// same command line, as an upgrade does, is the process it was; one that
// keeps doing it is the line a crash loop gets.
//
// It says restarting rather than crashing, because the process table cannot
// tell the two apart. A daemon something reloads on a schedule just before
// every report, as logrotate does, gets the line as well, and the line is
// true of it. A crash loop that changes its command line each time is a
// different invocation, and is gone once rather than restarting.
func restarting(prev, curr []ProcessIdentity, since time.Duration) []Change {
	if since <= 0 {
		return nil
	}
	restarted := map[ProcessIdentity]bool{}
	for _, p := range prev {
		if p.Young == youngRestarted {
			restarted[p.key()] = true
		}
	}
	var out []Change
	for _, c := range curr {
		if c.Young == "" || c.age+etimeSlack >= since || !restarted[c.key()] {
			continue
		}
		out = append(out, Change{
			Kind:    kindState,
			Subject: c.Name,
			Detail:  "restarting — under a minute old at this report and the last",
			noun:    nounProcesses,
		})
	}
	return out
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
func attentionFromChanges(prev, snap *Snapshot) []Finding {
	var out []Finding

	for _, p := range newlyPublicListeners(prev.Ports, snap.Ports) {
		who := owner(p)
		if who == "" {
			who = unidentified
		}
		out = append(out, Finding{
			Kind:   kindPort,
			Target: ":" + p.Port + "/" + p.Protocol,
			Text: fmt.Sprintf(
				"Port %s is now reachable from every interface, answered by %s — it was not before",
				":"+p.Port+"/"+p.Protocol, who),
		})
	}

	for _, p := range publicPortsChangedHands(prev.Ports, snap.Ports) {
		out = append(out, Finding{
			Kind:   kindPort,
			Target: ":" + p.Port + "/" + p.Protocol,
			Text: fmt.Sprintf(
				"Port %s is answered by %s now, and was not at the last report",
				":"+p.Port+"/"+p.Protocol, owner(p)),
		})
	}

	var stopped []string
	for _, c := range containersNeedingAttention(prev, snap) {
		p := indexContainers(prev.Containers)[c.Name]
		switch {
		case p.ID != c.ID:
			out = append(out, Finding{
				Kind:   "container",
				Target: c.Name,
				Text: fmt.Sprintf(
					"%s was recreated and is %s, not running — the deploy did not come back", c.Name, c.State),
			})
		case p.Image != c.Image:
			out = append(out, Finding{
				Kind:   "container",
				Target: c.Name,
				Text:   fmt.Sprintf("%s took a new image and is %s, not running", c.Name, c.State),
			})
		default:
			stopped = append(stopped, c.Name)
		}
	}

	// A reboot stops everything at once. Naming thirty containers one per
	// line turns the section a person reads first into the longest one on the
	// page, which is the failure this whole feature is trying to avoid.
	if len(stopped) > groupThreshold {
		// The collapsed line is about all of them, so it names none of them
		// as its target: a caller that acted on the first name would act on
		// an arbitrary one.
		out = append(out, Finding{
			Kind: "container",
			Text: fmt.Sprintf("%d containers stopped since the last report: %s, +%d",
				len(stopped), strings.Join(stopped[:groupNamed], ", "), len(stopped)-groupNamed),
		})
	} else {
		for _, name := range stopped {
			out = append(out, Finding{
				Kind:   "container",
				Target: name,
				Text:   name + " stopped since the last report",
			})
		}
	}

	// Sorted by the sentence, which is the order this section has always been
	// printed in.
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	return out
}

// containersNeedingAttention returns containers that were present at the last
// report and are not running now, in a stable order.
func containersNeedingAttention(prev, snap *Snapshot) []docker.Container {
	prevByName := indexContainers(prev.Containers)
	var out []docker.Container
	for _, c := range snap.Containers {
		p, existed := prevByName[c.Name]
		if !existed || c.State == "running" || p.State != "running" && p.ID == c.ID && p.Image == c.Image {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// actionsFromChanges names what to do and to what.
//
// The old actions compared counts, so they could not say which port or which
// container, and a port that changed hands without changing the count
// produced no action at all — the same blind spot #58 closed one section
// above, still sitting here.
func actionsFromChanges(prev, snap *Snapshot) []Action {
	var out []Action

	for _, p := range newlyPublicListeners(prev.Ports, snap.Ports) {
		who := owner(p)
		if who == "" {
			who = unidentified
		}
		out = append(out, action(fmt.Sprintf("Verify %s should be reachable from every interface, answered by %s.",
			":"+p.Port+"/"+p.Protocol, who)))
	}

	for _, p := range publicPortsChangedHands(prev.Ports, snap.Ports) {
		out = append(out, action(fmt.Sprintf("Verify %s should be answering from %s.",
			":"+p.Port+"/"+p.Protocol, owner(p))))
	}

	stopped := containersNeedingAttention(prev, snap)
	if len(stopped) > groupNamed {
		stopped = stopped[:groupNamed]
	}
	for _, c := range stopped {
		out = append(out, action(fmt.Sprintf("Check why %s is not running: homebutler docker logs %s", c.Name, c.Name)))
	}

	return out
}

// publicPortsChangedHands reports listeners reachable from every interface
// whose answering process changed.
//
// The count-based action went silent here: the same number of public ports,
// a different service behind one of them. That is the blind spot #58 closed
// for the change list, and it sat one section lower until #137.
func publicPortsChangedHands(prev, curr []ports.PortInfo) []ports.PortInfo {
	before := indexPorts(prev)
	var out []ports.PortInfo
	// 0.0.0.0 and [::] are two listeners for one port a reader thinks of as
	// one, and each produced its own finding and its own action.
	seen := map[string]bool{}
	for key, c := range indexPorts(curr) {
		p, existed := before[key]
		if !existed || !ports.IsPublicBind(c.Address) || seen[c.Port+"/"+c.Protocol] {
			continue
		}
		if owner(p) != "" && owner(c) != "" && ownerIdentity(p) != ownerIdentity(c) {
			seen[c.Port+"/"+c.Protocol] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
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
