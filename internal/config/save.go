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

	edits := map[string]string{}
	if patch.Alerts != nil {
		for key, value := range map[string]*float64{
			"cpu":    patch.Alerts.CPU,
			"memory": patch.Alerts.Memory,
			"disk":   patch.Alerts.Disk,
		} {
			if value != nil {
				edits["alerts."+key] = formatNumber(*value)
			}
		}
	}

	lines := splitLines(current)
	root := rootMapping(&doc)

	// Applied deepest line first so that an insertion does not move the line
	// another edit was found at.
	type placement struct {
		line  int // 0-based; -1 means append a new section
		col   int // 1-based column of the value, 0 when inserting
		text  string
		key   string
		under string
	}
	var work []placement

	for dotted, value := range edits {
		section, key := splitKey(dotted)
		mapping := lookup(root, section)

		switch {
		case mapping != nil && lookup(mapping, key) != nil:
			node := lookup(mapping, key)
			work = append(work, placement{line: node.Line - 1, col: node.Column, text: value})
		case mapping != nil:
			// The section exists and the key does not: put it at the end of
			// that section, indented like its siblings.
			work = append(work, placement{line: sectionEnd(mapping, lines), col: 0, text: value, key: key, under: section})
		default:
			work = append(work, placement{line: -1, col: 0, text: value, key: key, under: section})
		}
	}

	sort.Slice(work, func(i, j int) bool { return work[i].line > work[j].line })

	indent := indentOf(current)
	for _, w := range work {
		switch {
		case w.col > 0:
			lines[w.line] = replaceValue(lines[w.line], w.col, w.text)
		case w.line >= 0:
			entry := strings.Repeat(" ", indent) + w.key + ": " + w.text
			lines = append(lines[:w.line+1], append([]string{entry}, lines[w.line+1:]...)...)
		default:
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, w.under+":", strings.Repeat(" ", indent)+w.key+": "+w.text)
		}
	}

	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out), nil
}

// splitLines keeps the file's line structure without inventing a trailing
// empty element for the final newline.
func splitLines(data []byte) []string {
	text := string(data)
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func splitKey(dotted string) (section, key string) {
	i := strings.Index(dotted, ".")
	return dotted[:i], dotted[i+1:]
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
	// A value spanning several lines ends where the next non-indented line
	// begins; walking forward over indented lines finds it without reparsing.
	for last+1 < len(lines) {
		next := lines[last+1]
		if strings.TrimSpace(next) == "" || !strings.HasPrefix(next, " ") {
			break
		}
		last++
	}
	return last
}

// replaceValue rewrites the value at col, keeping anything the line carries
// after it. The values written here are numbers, so a # can only be the start
// of a comment.
func replaceValue(line string, col int, value string) string {
	if col-1 > len(line) {
		return line
	}
	prefix := line[:col-1]
	rest := line[col-1:]

	trailer := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		trailer = rest[i:]
		spacing := rest[:i]
		gap := len(spacing) - len(strings.TrimRight(spacing, " "))
		trailer = strings.Repeat(" ", gap) + trailer
	}
	return prefix + value + trailer
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// The new file has to be one homebutler would accept, or a save could
	// produce something config validate then rejects.
	if result := Validate(tmpName); result.Errors() > 0 {
		return fmt.Errorf("refusing to save: the result would be invalid (%d error(s))", result.Errors())
	}

	return os.Rename(tmpName, target)
}
