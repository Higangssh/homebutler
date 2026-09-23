package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

// maxTierBody caps what is read looking for a confirmation. These bodies carry
// a flag and a name; anything larger is not one of ours and reading it would
// be a way to make the server hold megabytes on request.
const maxTierBody = 64 << 10

// readBody reads the request body and puts it back, so the handler behind the
// tier check still sees it. Without this, checking the confirmation would
// consume the thing the handler was about to act on.
func readBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxTierBody))
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data
}

func bodyFields(r *http.Request) map[string]any {
	data := readBody(r)
	if len(data) == 0 {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil
	}
	return fields
}

// confirmed reports whether the caller said the destructive thing is what they
// meant. Only a real boolean true counts: the string "true" is what a form
// sends by accident, and a tier exists to be deliberate.
func confirmed(r *http.Request) bool {
	v, ok := bodyFields(r)["confirm"].(bool)
	return ok && v
}

// confirmName is the target the caller typed back.
func confirmName(r *http.Request) string {
	switch v := bodyFields(r)["confirm_name"].(type) {
	case string:
		return v
	case float64:
		// A vmid arrives as a number when the client did not quote it.
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// bodyString reads one string field, for a target named in the body rather
// than in the path.
func bodyString(r *http.Request, key string) string {
	v, _ := bodyFields(r)[key].(string)
	return v
}

// bodyStrings reads a JSON array of strings from the request body, and takes a
// bare string as a list of one.
func bodyStrings(r *http.Request, key string) []string {
	switch val := bodyFields(r)[key].(type) {
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}
