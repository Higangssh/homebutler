// Package schema stamps the JSON documents 1.0 freezes with the version of
// their shape.
//
// A field cannot be added to a frozen document after the freeze without a
// caller having to guess whether its absence means "old homebutler" or "not
// set". So the field goes in before the freeze, and from then on a caller can
// branch on a number instead of on which keys happen to be present.
package schema

import "strconv"

// Current is the version of every document that carries a Version. It moves
// only with a major release, and the frozen documents move together.
const Current = 1

// Version is a document's schema_version field.
//
// It always marshals as Current, whatever value it holds. The number describes
// the shape being written, and the shape being written is always the current
// struct — including when that struct was decoded from an older snapshot and
// is being written again. Making the zero value correct means no constructor,
// demo fixture or test helper can emit a document that claims version 0.
//
// It decodes normally, so a document read back says which version wrote it.
type Version int

// MarshalJSON writes Current.
func (Version) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(Current)), nil
}
