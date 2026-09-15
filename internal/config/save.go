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
type Patch struct {
	Alerts *AlertsPatch
}

// IsEmpty reports whether the patch would change nothing.
func (p Patch) IsEmpty() bool {
	return p.Alerts == nil || (p.Alerts.CPU == nil && p.Alerts.Memory == nil && p.Alerts.Disk == nil)
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

type keyValue struct{ key, value string }

type sectionEdit struct {
	name string
	keys []keyValue
}

// collectEdits turns a patch into the sections and keys to write, in a fixed
// order so the same patch always produces the same file.
func collectEdits(patch Patch) []sectionEdit {
	var out []sectionEdit
	if patch.Alerts != nil {
		var keys []keyValue
		for _, kv := range []struct {
			key   string
			value *float64
		}{
			{"cpu", patch.Alerts.CPU},
			{"memory", patch.Alerts.Memory},
			{"disk", patch.Alerts.Disk},
		} {
			if kv.value != nil {
				keys = append(keys, keyValue{kv.key, formatNumber(*kv.value)})
			}
		}
		if len(keys) > 0 {
			out = append(out, sectionEdit{name: "alerts", keys: keys})
		}
	}
	return out
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

	// An insertion moves every line below it, so replacements are applied
	// first and insertions from the bottom up.
	type insertion struct {
		after  int // 0-based line to insert after; -1 appends a new section
		indent int
		lines  []string
	}
	var inserts []insertion

	for _, section := range collectEdits(patch) {
		mapping := lookup(root, section.name)
		if mapping != nil && mapping.Style == yaml.FlowStyle {
			return nil, fmt.Errorf("%w: %s", ErrFlowStyle, section.name)
		}

		if mapping == nil {
			// The whole section is new: written once, however many keys it
			// carries. Appending per key produced a duplicate mapping, which
			// Validate then refused.
			indent := indentOf(current)
			block := []string{section.name + ":"}
			for _, kv := range section.keys {
				block = append(block, strings.Repeat(" ", indent)+kv.key+": "+kv.value)
			}
			inserts = append(inserts, insertion{after: -1, lines: block})
			continue
		}

		indent := siblingIndent(mapping, indentOf(current))
		var missing []string
		for _, kv := range section.keys {
			if node := lookup(mapping, kv.key); node != nil {
				lines[node.Line-1] = replaceValue(lines[node.Line-1], node.Column, kv.value)
				continue
			}
			missing = append(missing, strings.Repeat(" ", indent)+kv.key+": "+kv.value)
		}
		if len(missing) > 0 {
			inserts = append(inserts, insertion{after: sectionEnd(mapping, lines), indent: indent, lines: missing})
		}
	}

	sort.Slice(inserts, func(i, j int) bool { return inserts[i].after > inserts[j].after })

	for _, ins := range inserts {
		if ins.after < 0 {
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, ins.lines...)
			continue
		}
		lines = append(lines[:ins.after+1], append(ins.lines, lines[ins.after+1:]...)...)
	}

	ending := lineEnding(current)
	return []byte(strings.Join(lines, ending) + ending), nil
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
func siblingIndent(mapping *yaml.Node, fallback int) int {
	if len(mapping.Content) > 0 {
		if col := mapping.Content[0].Column - 1; col > 0 {
			return col
		}
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
func sectionEnd(mapping *yaml.Node, lines []string) int {
	last := mapping.Line - 1
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if line := mapping.Content[i+1].Line - 1; line > last {
			last = line
		}
	}
	for last+1 < len(lines) {
		next := lines[last+1]
		if strings.TrimSpace(next) == "" || !strings.HasPrefix(next, " ") {
			break
		}
		last++
	}
	return last
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
