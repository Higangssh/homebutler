package schema

import (
	"encoding/json"
	"testing"
)

type document struct {
	SchemaVersion Version `json:"schema_version"`
}

// The zero value is what every constructor that forgot the field produces, so
// it has to be the one that writes the right number.
func TestAnUnsetVersionIsWrittenAsCurrent(t *testing.T) {
	out, err := json.Marshal(document{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"schema_version":1}` {
		t.Errorf("got %s", out)
	}
}

// Reading back says which version wrote the document; writing it again says
// which shape is being written now.
func TestADecodedVersionIsReadAsWrittenAndWrittenAsCurrent(t *testing.T) {
	var old document
	if err := json.Unmarshal([]byte(`{"schema_version":0}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.SchemaVersion != 0 {
		t.Errorf("decoded %d, want 0", old.SchemaVersion)
	}
	out, _ := json.Marshal(old)
	if string(out) != `{"schema_version":1}` {
		t.Errorf("re-encoded as %s", out)
	}
}
