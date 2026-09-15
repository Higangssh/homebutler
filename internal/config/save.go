package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Higangssh/homebutler/internal/notify"

	"gopkg.in/yaml.v3"
)

// ErrStale is returned when the file changed after the revision being saved
// against was taken.
var ErrStale = errors.New("the config file changed on disk since it was read")

// Revision identifies the config file as it was when it was read.
//
// Someone with the dashboard open and an editor in another window is the
// ordinary case, not an edge one: without this, saving a threshold from the
// browser silently discards whatever they wrote in vim. A save carries the
// revision it was made against and is refused if the file has moved on.
type Revision struct {
	sum string // sha-256 of the bytes; empty means the file did not exist
}

// Exists reports whether the file was there when the revision was taken.
func (r Revision) Exists() bool { return r.sum != "" }

// String is the form that crosses to a browser and comes back with the save it
// was made against.
func (r Revision) String() string { return r.sum }

// ParseRevision reads back what String wrote. An empty string is the revision
// of a file that was not there, which is how creating one is expressed.
func ParseRevision(s string) (Revision, error) {
	if s == "" {
		return Revision{}, nil
	}
	if len(s) != 64 {
		return Revision{}, errors.New("not a config revision")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return Revision{}, errors.New("not a config revision")
	}
	return Revision{sum: s}, nil
}

// ReadRevision returns the revision of the file at path. A file that does not
// exist has a revision too, so that creating one can also be refused if
// something else created it first.
func ReadRevision(path string) (Revision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Revision{}, nil
		}
		return Revision{}, fmt.Errorf("failed to read config: %w", err)
	}
	return revisionOf(data), nil
}

func revisionOf(data []byte) Revision {
	sum := sha256.Sum256(data)
	return Revision{sum: hex.EncodeToString(sum[:])}
}

// AlertsPatch changes alert thresholds. A nil field is not written.
type AlertsPatch struct {
	CPU    *float64
	Memory *float64
	Disk   *float64
}

// Patch is the set of settings to change.
//
// It is a patch of the fields being changed rather than a whole document, and
// that is a decision about secrets rather than a convenience. /api/config
// redacts a password to bullets, so a caller that sent back what it was given
// would write those bullets into the file as the password. A field nobody set
// is not written, so a secret nobody is changing cannot be damaged by a save
// that was about something else.
// NotifyPatch changes one notification channel.
//
// Values carries the keys that are not credentials; a key that is absent is
// left as it is. Secrets carries the ones that are, because those have three
// states rather than two. Remove deletes the whole block.
type NotifyPatch struct {
	Values  map[string]string
	Secrets map[string]*Secret
	Remove  bool
}

// WakePatch adds or removes one Wake-on-LAN target by name.
type WakePatch struct {
	Name      string
	MAC       string
	Broadcast string
	Remove    bool
}

// ServerPatch changes one server, addressed by the name it has now. A field
// left nil is not written, so a save about the port cannot disturb the user.
//
// KeyFile is deliberately absent. It names a path on the machine homebutler
// runs on, and a browser is the wrong place to be choosing one; the CLI still
// edits it.
type ServerPatch struct {
	Name     string
	Rename   string
	Host     *string
	Port     *int
	User     *string
	AuthMode *string
	Password *Secret
	Remove   bool
}

// ProxmoxPatch changes one Proxmox endpoint. The read credential and the one
// that performs guest actions are separate on purpose, and stay separate here.
//
// The *_file paths are absent for the same reason KeyFile is.
type ProxmoxPatch struct {
	Name          string
	Rename        string
	Host          *string
	Port          *int
	TokenID       *string
	Token         *Secret
	ActionTokenID *string
	ActionToken   *Secret
	Remove        bool
}

type Patch struct {
	Alerts *AlertsPatch
	// Notify is keyed by channel name, as internal/notify spells it.
	Notify  map[string]*NotifyPatch
	Wake    []WakePatch
	Servers []ServerPatch
	Proxmox []ProxmoxPatch
}

// IsEmpty reports whether the patch would change nothing.
func (p Patch) IsEmpty() bool {
	if p.Alerts != nil && (p.Alerts.CPU != nil || p.Alerts.Memory != nil || p.Alerts.Disk != nil) {
		return false
	}
	return len(p.Notify) == 0 && len(p.Wake) == 0 && len(p.Servers) == 0 && len(p.Proxmox) == 0
}

// Validate reports what is wrong with the patch itself, before any file is
// read. A channel or key homebutler does not have is a caller mistake rather
// than a config problem, and saying so here keeps it out of the file.
func (p Patch) Validate() error {
	for name, channel := range p.Notify {
		fields, ok := notify.FieldsFor(notify.Channel(name))
		if !ok {
			return fmt.Errorf("%q is not a notification channel", name)
		}
		if channel.Remove {
			continue
		}
		known := map[string]bool{}
		secret := map[string]bool{}
		for _, f := range fields {
			known[f.Name] = true
			secret[f.Name] = f.Secret
		}
		for key := range channel.Values {
			switch {
			case !known[key]:
				return fmt.Errorf("%s has no setting called %q", name, key)
			case secret[key]:
				// Sending a credential as a plain value would put it in the
				// two-state world where an empty box means "clear".
				return fmt.Errorf("%s.%s is a credential and has to be sent as one", name, key)
			}
		}
		for key := range channel.Secrets {
			if !known[key] {
				return fmt.Errorf("%s has no setting called %q", name, key)
			}
			if !secret[key] {
				return fmt.Errorf("%s.%s is not a credential", name, key)
			}
		}
	}
	for _, target := range p.Wake {
		if target.Name == "" {
			return errors.New("a wake target needs a name")
		}

		if target.Remove {
			continue
		}
		if err := CheckWakeTarget(target.Name, target.MAC); err != nil {
			return err
		}
	}

	for _, server := range p.Servers {
		if server.Name == "" {
			return errors.New("a server needs a name")
		}
		if server.AuthMode != nil && *server.AuthMode != "key" && *server.AuthMode != "password" {
			return fmt.Errorf("%q is not a way to sign in; it is either key or password", *server.AuthMode)
		}
		if server.Port != nil && (*server.Port < 1 || *server.Port > 65535) {
			return fmt.Errorf("%d is not a port", *server.Port)
		}
	}

	for _, endpoint := range p.Proxmox {
		if endpoint.Name == "" {
			return errors.New("a Proxmox endpoint needs a name")
		}
		if endpoint.Port != nil && (*endpoint.Port < 1 || *endpoint.Port > 65535) {
			return fmt.Errorf("%d is not a port", *endpoint.Port)
		}
	}
	return nil
}

// CheckWakeTarget reports why a target cannot be saved, in the words config
// validate uses for the same mistake.
//
// It is one function rather than a rule written twice because a save and a
// `config validate` that disagreed about what a MAC looks like would be two
// answers to one question — and the disagreement would only surface after the
// packet had gone nowhere. Wake-on-LAN is fire and forget: nothing replies, so
// a typed address shows up as a machine that simply did not turn on, which is
// exactly the case a form has to catch before it writes.
func CheckWakeTarget(name, mac string) error {
	if name == "" {
		return errors.New("a wake target needs a name")
	}
	message, hint := macProblem(mac)
	switch {
	case message == "":
		return nil
	case hint == "":
		return errors.New(lowerFirst(message))
	default:
		return errors.New(lowerFirst(message) + " " + hint)
	}
}

// lowerFirst starts a sentence the way an error is expected to: Go's errors
// are lowercase because they are wrapped into longer ones, and config
// validate's findings are sentences on their own.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return strings.TrimSuffix(string(r), ".")
}

// Save applies patch to the config file at path.
//
// The file is edited rather than regenerated: it is written by a person, and
// keeps their comments, their key order, the keys this version of homebutler
// does not recognise, and their indentation. Only the values the patch names
// are touched.
//
// #154 calls it as:
//
//	rev, _ := config.ReadRevision(path)
//	err := config.Save(path, rev, config.Patch{Alerts: &config.AlertsPatch{CPU: &v}})
func Save(path string, rev Revision, patch Patch) error {
	if path == "" {
		return errors.New("no config path")
	}
	if patch.IsEmpty() {
		return errors.New("nothing to change")
	}
	if err := patch.Validate(); err != nil {
		return err
	}

	// The current revision is computed the way ReadRevision computes it,
	// including for a file that is not there, so the two are comparable
	// without a special case for creating one.
	var current []byte
	var currentRev Revision
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		current, currentRev = data, revisionOf(data)
	case os.IsNotExist(err):
	default:
		return fmt.Errorf("failed to read config: %w", err)
	}

	if currentRev != rev {
		return ErrStale
	}

	// Whether a change moves a credential depends on where the item points
	// now, so this is checked against the file as it is rather than against
	// the patch alone.
	if len(patch.Servers) > 0 || len(patch.Proxmox) > 0 {
		var onDisk Config
		if err := yaml.Unmarshal(current, &onDisk); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
		if err := checkDestinations(&onDisk, patch); err != nil {
			return err
		}
	}

	edited, err := applyPatch(current, patch)
	if err != nil {
		return err
	}

	return writeAtomic(path, edited)
}

// ErrFlowStyle is returned for a section written on one line. Editing text at
// a position cannot change one value inside `alerts: {cpu: 90}` without
// rewriting the line, and rewriting it would lose whatever else it holds.
var ErrFlowStyle = errors.New("that section is written on one line, which homebutler cannot edit in place")

// step is one move along a path: into a key of a mapping, or into the item of
// a sequence. Both are needed because homebutler's config is both — `alerts`
// is a mapping of thresholds, `wake` is a list of machines — and an editor
// that could only walk mappings could reach a threshold but not a target.
//
// A sequence item is addressed by the name its owner gave it rather than by
// its position. Every list in this config is a list of named things, it is how
// the CLI and the dashboard already refer to them, and a save that meant "the
// second one" would rewrite the wrong machine after somebody reordered the
// file in an editor.
type step struct {
	key  string // a key of a mapping
	name string // the item of a sequence whose name field holds this
}

func mapKey(key string) step   { return step{key: key} }
func seqItem(name string) step { return step{name: name} }

func (s step) isItem() bool { return s.key == "" }

func (s step) String() string {
	if s.isItem() {
		return "[" + s.name + "]"
	}
	return s.key
}

// pathString names a path in a message someone has to act on.
func pathString(path []step) string {
	var b strings.Builder
	for i, s := range path {
		if i > 0 && !s.isItem() {
			b.WriteString(".")
		}
		b.WriteString(s.String())
	}
	return b.String()
}

// edit is one value to write, addressed by its path from the root:
// ["alerts", "cpu"], ["notify", "ntfy", "topic"], or ["wake", [nas], "mac"].
// A path rather than a section and key because notify nests one level deeper
// than alerts, and the next thing to be editable will nest somewhere else
// again.
type edit struct {
	path  []step
	value string
}

// removal is a whole mapping — a notification channel, a wake target — to
// delete, addressed the same way.
type removal struct{ path []step }

// collectEdits turns a patch into paths and values in a fixed order, so the
// same patch always produces the same file.
func collectEdits(patch Patch) ([]edit, []removal) {
	var edits []edit
	var removals []removal

	if patch.Alerts != nil {
		for _, kv := range []struct {
			key   string
			value *float64
		}{
			{"cpu", patch.Alerts.CPU},
			{"memory", patch.Alerts.Memory},
			{"disk", patch.Alerts.Disk},
		} {
			if kv.value != nil {
				edits = append(edits, edit{path: []step{mapKey("alerts"), mapKey(kv.key)}, value: formatNumber(*kv.value)})
			}
		}
	}

	for _, name := range sortedKeys(patch.Notify) {
		channel := patch.Notify[name]
		if channel.Remove {
			removals = append(removals, removal{path: []step{mapKey("notify"), mapKey(name)}})
			continue
		}
		fields, _ := notify.FieldsFor(notify.Channel(name))
		// Written in the order the channel declares, so a new block reads the
		// way the documentation does.
		for _, f := range fields {
			if f.Secret {
				if value, write := channel.Secrets[f.Name].resolve(); write {
					edits = append(edits, edit{path: []step{mapKey("notify"), mapKey(name), mapKey(f.Name)}, value: quoteIfNeeded(value)})
				}
				continue
			}
			if value, ok := channel.Values[f.Name]; ok {
				edits = append(edits, edit{path: []step{mapKey("notify"), mapKey(name), mapKey(f.Name)}, value: quoteIfNeeded(value)})
			}
		}
	}

	for _, target := range patch.Wake {
		if target.Remove {
			removals = append(removals, removal{path: []step{mapKey("wake"), seqItem(target.Name)}})
			continue
		}
		// name comes first, because it is what the item is addressed by and a
		// new target reads as a list of machines rather than of addresses.
		// The broadcast address is written under `ip`, which is what the key
		// is called in the file. The dashboard and the JSON call it broadcast,
		// because that is what it is, and the writer has to speak the file's
		// spelling rather than the API's.
		for _, kv := range []struct{ key, value string }{
			{"name", target.Name},
			{"mac", target.MAC},
			{"ip", target.Broadcast},
		} {
			if kv.value == "" && kv.key != "name" {
				continue
			}
			edits = append(edits, edit{
				path:  []step{mapKey("wake"), seqItem(target.Name), mapKey(kv.key)},
				value: quoteIfNeeded(kv.value),
			})
		}
	}

	for _, server := range patch.Servers {
		if server.Remove {
			removals = append(removals, removal{path: []step{mapKey("servers"), seqItem(server.Name)}})
			continue
		}

		// The item is addressed by the name it has in the file, and renaming is
		// writing that one key: a rename is an edit rather than a delete and an
		// add, so everything else about the server stays where it is, comments
		// included. A server that is not being renamed does not have its name
		// rewritten — that would requote a name somebody wrote their own way.
		item := func(key, value string) edit {
			return edit{path: []step{mapKey("servers"), seqItem(server.Name), mapKey(key)}, value: value}
		}
		if server.Rename != "" {
			edits = append(edits, item("name", quoteIfNeeded(server.Rename)))
		}
		if server.Host != nil {
			edits = append(edits, item("host", quoteIfNeeded(*server.Host)))
		}
		if server.Port != nil {
			edits = append(edits, item("port", strconv.Itoa(*server.Port)))
		}
		if server.User != nil {
			edits = append(edits, item("user", quoteIfNeeded(*server.User)))
		}
		if server.AuthMode != nil {
			edits = append(edits, item("auth", quoteIfNeeded(*server.AuthMode)))
		}
		if value, write := server.Password.resolve(); write {
			edits = append(edits, item("password", quoteIfNeeded(value)))
		}
	}

	for _, endpoint := range patch.Proxmox {
		if endpoint.Remove {
			removals = append(removals, removal{path: []step{mapKey("proxmox"), seqItem(endpoint.Name)}})
			continue
		}

		item := func(key, value string) edit {
			return edit{path: []step{mapKey("proxmox"), seqItem(endpoint.Name), mapKey(key)}, value: value}
		}
		if endpoint.Rename != "" {
			edits = append(edits, item("name", quoteIfNeeded(endpoint.Rename)))
		}
		if endpoint.Host != nil {
			edits = append(edits, item("host", quoteIfNeeded(*endpoint.Host)))
		}
		if endpoint.Port != nil {
			edits = append(edits, item("port", strconv.Itoa(*endpoint.Port)))
		}
		if endpoint.TokenID != nil {
			edits = append(edits, item("token_id", quoteIfNeeded(*endpoint.TokenID)))
		}
		if value, write := endpoint.Token.resolve(); write {
			edits = append(edits, item("token", quoteIfNeeded(value)))
		}
		if endpoint.ActionTokenID != nil {
			edits = append(edits, item("action_token_id", quoteIfNeeded(*endpoint.ActionTokenID)))
		}
		if value, write := endpoint.ActionToken.resolve(); write {
			edits = append(edits, item("action_token", quoteIfNeeded(value)))
		}
	}

	return edits, removals
}

func sortedKeys(m map[string]*NotifyPatch) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// quoteIfNeeded writes a value YAML will read back as the same string. A token
// is the case that matters: one starting with a digit and containing a colon
// parses as something other than a string if it is left bare.
func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, ":#{}[],&*?|>'\"%@`") || strings.TrimSpace(value) != value {
		return strconv.Quote(value)
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return strconv.Quote(value)
	}
	switch strings.ToLower(value) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return strconv.Quote(value)
	}
	return value
}

// applyPatch rewrites only the lines the patch names.
//
// The document is parsed to find where each value lives and then the original
// text is edited at that position, rather than being re-encoded from the parse
// tree. Re-encoding keeps comments and unknown keys, but it also drops the
// blank lines between sections and re-emits every line, so a request to change
// one threshold arrives as a diff touching the whole file. A person wrote this
// file; a save should read like something they did.
func applyPatch(current []byte, patch Patch) ([]byte, error) {
	edits, removals := collectEdits(patch)
	return applyEdits(current, edits, removals)
}

// applyEdits is the editor itself, with nothing in it that knows what a wake
// target or a notification channel is: it writes values at paths and deletes
// what paths name. Keeping it separate from collectEdits is what lets a
// section become editable by describing it rather than by teaching this code
// about it, and it is how the machine is tested against shapes no Patch can
// build yet.
func applyEdits(current []byte, edits []edit, removals []removal) ([]byte, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(current)) > 0 {
		if err := yaml.Unmarshal(current, &doc); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	lines := splitLines(current)
	root := rootMapping(&doc)
	if root != nil && root.Style == yaml.FlowStyle {
		return nil, ErrFlowStyle
	}

	indent := indentOf(current)

	// Deletions and insertions both move the lines below them, so everything
	// is collected first and applied from the bottom up.
	type change struct {
		at    int      // 0-based line to act on or insert after; -1 appends
		drop  int      // lines to remove at `at`
		lines []string // lines to insert after `at`
	}
	var changes []change

	for _, r := range removals {
		node, parent, flow := resolve(root, r.path)
		if flow {
			return nil, fmt.Errorf("%w: %s", ErrFlowStyle, pathString(r.path))
		}
		if node == nil {
			continue // already absent
		}
		// A key is deleted from the line its key is on; a sequence item from
		// the line its dash is on, which is the line its first key shares.
		first := node.Line - 1
		if last := r.path[len(r.path)-1]; !last.isItem() {
			first = keyLineOf(parent, last.key)
		}
		changes = append(changes, change{at: first, drop: sectionEnd(node, lines) - first + 1})
	}

	// Sections created by this patch, so two keys for the same new channel do
	// not each write the block.
	created := map[string]bool{}

	for _, e := range edits {
		node, _, flow := resolve(root, e.path)
		if flow {
			return nil, fmt.Errorf("%w: %s", ErrFlowStyle, pathString(e.path))
		}
		if node != nil {
			lines[node.Line-1] = replaceValue(lines[node.Line-1], node.Column, e.value)
			continue
		}

		parentPath := e.path[:len(e.path)-1]
		key := e.path[len(e.path)-1]
		parent, _, parentFlow := resolve(root, parentPath)
		if parentFlow {
			return nil, fmt.Errorf("%w: %s", ErrFlowStyle, pathString(parentPath))
		}

		if parent != nil && parent.Kind == yaml.MappingNode {
			at := sectionEnd(parent, lines)
			col := containerIndent(parent, lines, len(parentPath)*indent)
			changes = append(changes, change{at: at, lines: []string{strings.Repeat(" ", col) + key.key + ": " + e.value}})
			continue
		}

		// Part of the path may already be there. Writing all of it again put a
		// second `notify:` in the file, which is what adding a second
		// notification channel did: the save was refused because the result no
		// longer parsed, and the channel could not be added at all. Only the
		// missing part is written, under the deepest key that does exist.
		joined := pathString(parentPath)
		if created[joined] {
			continue
		}
		created[joined] = true

		anchor, depth := deepestExisting(root, parentPath)
		switch {
		case anchor == nil:
			// None of it is there: the whole path goes at the end of the file.
			changes = append(changes, change{at: -1, lines: newBlock(parentPath, parentPath, edits, indent, 0)})
		case anchor.Kind == yaml.MappingNode || anchor.Kind == yaml.SequenceNode:
			at := sectionEnd(anchor, lines)
			base := containerIndent(anchor, lines, depth*indent)
			changes = append(changes, change{at: at, lines: newBlock(parentPath, parentPath[depth:], edits, indent, base)})
		default:
			// The key is there with nothing under it — a `notify:` left behind
			// when its last channel was removed, or written empty by hand. Its
			// children belong on the lines after it, one level in.
			changes = append(changes, change{at: anchor.Line - 1, lines: newBlock(parentPath, parentPath[depth:], edits, indent, depth*indent)})
		}
	}

	sort.SliceStable(changes, func(i, j int) bool { return changes[i].at > changes[j].at })

	for _, c := range changes {
		switch {
		case c.drop > 0:
			lines = append(lines[:c.at], lines[c.at+c.drop:]...)
		case c.at < 0:
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, c.lines...)
		default:
			lines = append(lines[:c.at+1], append(c.lines, lines[c.at+1:]...)...)
		}
	}

	ending := lineEnding(current)
	return []byte(strings.Join(lines, ending) + ending), nil
}

// newBlock writes the part of a path that is not in the file yet, with every
// key of the patch that belongs under it.
//
// full is the whole path, which is what the other keys of the patch are matched
// against; missing is the part that has to be created; base is how far the
// first created level is indented, so a block written under a key that already
// exists lines up with the keys beside it.
func newBlock(full, missing []step, all []edit, indent, base int) []string {
	var block []string
	col := base

	for _, s := range missing {
		if !s.isItem() {
			block = append(block, strings.Repeat(" ", col)+s.key+":")
			col += indent
			continue
		}
		// A sequence item opens with a dash, and its keys line up after it.
		// The name is written here rather than left to the loop below, because
		// it is the key the dash has to share a line with.
		block = append(block, strings.Repeat(" ", col)+"- name: "+quoteIfNeeded(s.name))
		col += 2
	}

	for _, other := range all {
		if len(other.path) != len(full)+1 || pathString(other.path[:len(full)]) != pathString(full) {
			continue
		}
		key := other.path[len(other.path)-1]
		if key.key == "name" {
			continue // already on the dash line
		}
		block = append(block, strings.Repeat(" ", col)+key.key+": "+other.value)
	}
	return block
}

// deepestExisting returns the last step along the path that is actually in the
// file, and how much of the path it covers.
func deepestExisting(root *yaml.Node, path []step) (*yaml.Node, int) {
	for depth := len(path); depth > 0; depth-- {
		if node, _, _ := resolve(root, path[:depth]); node != nil {
			return node, depth
		}
	}
	return nil, 0
}

// resolve walks a path and returns the node it names along with the mapping or
// sequence that holds it, or nil when any step is missing.
//
// flow reports a mapping or a list written on one line anywhere along the way,
// not only at the end: a value inside `alerts: {cpu: 90}` has a position, and
// editing at it produces a broken line. Catching it here means the refusal
// names the real reason rather than arriving later as a parse error.
func resolve(root *yaml.Node, path []step) (node, parent *yaml.Node, flow bool) {
	current := root
	for i, s := range path {
		if current == nil {
			return nil, nil, false
		}
		if current.Style == yaml.FlowStyle {
			return nil, nil, true
		}

		var next *yaml.Node
		switch {
		case s.isItem():
			if current.Kind != yaml.SequenceNode {
				return nil, nil, false
			}
			next = itemNamed(current, s.name)
		default:
			if current.Kind != yaml.MappingNode {
				return nil, nil, false
			}
			next = lookup(current, s.key)
		}
		if next == nil {
			return nil, nil, false
		}
		if i == len(path)-1 {
			return next, current, next.Kind != yaml.ScalarNode && next.Style == yaml.FlowStyle
		}
		current = next
	}
	return nil, nil, false
}

// itemNamed finds the item of a sequence whose name field holds name. An item
// that is not a mapping, or has no name, is not addressable and is skipped
// rather than refused: it is somebody else's key in their file.
func itemNamed(sequence *yaml.Node, name string) *yaml.Node {
	for _, item := range sequence.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if value := lookup(item, "name"); value != nil && value.Value == name {
			return item
		}
	}
	return nil
}

func keyLineOf(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i].Line - 1
		}
	}
	return 0
}

// lineEnding is the one the file already uses. A config edited on Windows and
// copied to a Pi carries CRLF, and mixing the two in one file is the kind of
// thing that shows up as an unexplained diff much later.
func lineEnding(current []byte) string {
	if bytes.Contains(current, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// siblingIndent is the column the section's own keys sit at, so an added key
// lines up with them rather than with whatever the first indented line in the
// file happens to use.
// containerIndent is how far the children of a node are pushed in.
//
// A mapping is measured by the column of its first key. A sequence is measured
// by the line its first item starts on instead: the item's first key sits
// after the dash, and the dash is where the item begins.
func containerIndent(node *yaml.Node, lines []string, fallback int) int {
	if len(node.Content) == 0 {
		return fallback
	}
	first := node.Content[0]
	if node.Kind == yaml.SequenceNode {
		if line := first.Line - 1; line >= 0 && line < len(lines) {
			return indentWidth(lines[line])
		}
		return fallback
	}
	if col := first.Column - 1; col > 0 {
		return col
	}
	return fallback
}

// splitLines returns the file's lines with their endings removed, so that
// every line is handled the same way whether it came from a CRLF file or not
// and the ending is applied once, on the way out. Keeping the carriage returns
// on the lines meant the last line had none — it had been part of the final
// terminator — and inserting after it produced a file with both kinds.
func splitLines(data []byte) []string {
	text := strings.TrimSuffix(string(data), "\n")
	text = strings.TrimSuffix(text, "\r")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func rootMapping(doc *yaml.Node) *yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}

// sectionEnd is the last line the mapping occupies, so a new key lands inside
// it rather than after whatever follows.
func sectionEnd(node *yaml.Node, lines []string) int {
	last := node.Line - 1
	if node.Kind == yaml.SequenceNode {
		for _, item := range node.Content {
			if line := item.Line - 1; line > last {
				last = line
			}
		}
	} else {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if line := node.Content[i+1].Line - 1; line > last {
				last = line
			}
		}
	}

	// Only lines indented at least as far as this node's own children belong
	// to it. Extending over anything that merely began with a space swallowed
	// the key below: removing one notification channel deleted every channel
	// written after it, and the file was rewritten without them.
	inner := containerIndent(node, lines, node.Column-1)
	for last+1 < len(lines) {
		next := lines[last+1]
		if strings.TrimSpace(next) == "" || indentWidth(next) < inner {
			break
		}
		last++
	}
	return last
}

// indentWidth is how far a line is pushed in. YAML indents with spaces only,
// so counting them is the whole of it.
func indentWidth(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// replaceValue rewrites the value at col and keeps the rest of the line
// exactly: an inline comment, trailing spaces, and the carriage return of a
// CRLF file.
func replaceValue(line string, col int, value string) string {
	if col-1 > len(line) {
		return line
	}
	prefix, rest := line[:col-1], line[col-1:]

	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\t' || r == '#' || r == '\r' {
			end = i
			break
		}
	}
	return prefix + value + rest[end:]
}

func lookup(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// formatNumber writes a threshold the way a person would: 90 rather than 90.0,
// and 92.5 unchanged.
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

var indentPattern = regexp.MustCompile(`(?m)^( +)\S`)

// indentOf recovers the indentation the file already uses, so a save does not
// reformat every line of a file it was asked to change one value in. Two
// spaces is the default because it is what homebutler writes and what most
// YAML uses.
func indentOf(current []byte) int {
	if m := indentPattern.FindSubmatch(current); m != nil {
		if n := len(m[1]); n > 0 && n <= 8 {
			return n
		}
	}
	return 2
}

// writeAtomic replaces the file at path with data.
//
// The temp file is created beside the real target rather than beside the path
// given, because a config kept in a dotfiles repository is usually a symlink:
// renaming onto the link would replace it with a regular file and quietly
// detach it from the repository.
func writeAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}

	mode := os.FileMode(0o600)
	if info, err := os.Stat(target); err == nil {
		// Never loosen, and tighten only a file that was too open to hold a
		// secret in the first place.
		if perm := info.Mode().Perm(); perm&0o077 == 0 {
			mode = perm
		}
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".homebutler-config-*")
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	// Flushed before the rename: on the SD card in a Pi, a rename that lands
	// before the data does leaves an empty config after a power cut.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// The new file has to be one homebutler would accept, or a save could
	// produce something config validate then rejects.
	if result := Validate(tmpName); result.Errors() > 0 {
		return fmt.Errorf("refusing to save: %s", firstError(result))
	}

	return os.Rename(tmpName, target)
}

// firstError names what is wrong rather than counting, so #154 has something
// to put in front of a person.
func firstError(result *ValidationResult) string {
	for _, f := range result.Findings {
		if f.Severity != SeverityError {
			continue
		}
		if f.Field != "" {
			return f.Field + ": " + f.Message
		}
		return f.Message
	}
	return "the result would be invalid"
}

// Secret is a credential inside a patch, which has three states rather than
// two: absent, replaced, or cleared.
//
// An empty string cannot mean "clear". A form submitted with the token box
// left blank sends an empty string, and reading that as "delete the token"
// destroys a working configuration because someone changed a threshold on the
// same page. Clearing is its own call.
type Secret struct {
	set   bool
	clear bool
	value string
}

// SetSecret replaces the stored value.
func SetSecret(value string) *Secret { return &Secret{set: true, value: value} }

// ClearSecret removes the stored value. Explicit, and never the result of an
// empty input.
func ClearSecret() *Secret { return &Secret{clear: true} }

// resolve reports the text to write and whether to write anything at all.
func (s *Secret) resolve() (string, bool) {
	switch {
	case s == nil:
		return "", false
	case s.clear:
		return "", true
	case s.set:
		return s.value, true
	}
	return "", false
}
