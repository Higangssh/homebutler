package util

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type inner struct {
	Tags []string `json:"tags"`
}

type outer struct {
	Items    []inner           `json:"items"`
	Optional []string          `json:"optional,omitempty"`
	Labels   map[string]string `json:"labels"`
	Pointer  *inner            `json:"pointer"`
	When     time.Time         `json:"when"`
	Nested   inner             `json:"nested"`
	Skipped  []string          `json:"-"`
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(EmptyLists(v))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestEmptyListsWritesEmptyWhereNullWouldHaveBeen(t *testing.T) {
	got := marshal(t, outer{Items: []inner{{}}})
	want := `{"items":[{"tags":[]}],"labels":{},"pointer":null,"when":"0001-01-01T00:00:00Z","nested":{"tags":[]}}`
	if got != want {
		t.Errorf("\ngot  %s\nwant %s", got, want)
	}
}

// The top level is where alerts_history answered null on a machine with no
// history.
func TestEmptyListsAtTheTop(t *testing.T) {
	var none []inner
	if got := marshal(t, none); got != "[]" {
		t.Errorf("a nil list at the top = %s", got)
	}
	var noMap map[string]int
	if got := marshal(t, noMap); got != "{}" {
		t.Errorf("a nil map at the top = %s", got)
	}
	if got := marshal(t, &outer{}); got[0] != '{' {
		t.Errorf("a pointer to a struct = %s", got)
	}
}

// Absence is a meaning of its own: omitempty and a nil pointer say the thing
// is not there, and they are left saying it.
func TestEmptyListsLeavesAbsenceAlone(t *testing.T) {
	got := marshal(t, outer{})
	for _, absent := range []string{`"optional"`, `"Skipped"`} {
		if contains(got, absent) {
			t.Errorf("%s appeared: %s", absent, got)
		}
	}
	if !contains(got, `"pointer":null`) {
		t.Errorf("a nil pointer stopped being null: %s", got)
	}
}

func TestEmptyListsDoesNotChangeWhatItWasGiven(t *testing.T) {
	v := outer{Items: []inner{{}}}
	_ = EmptyLists(v)
	_ = EmptyLists(&v)
	if v.Items[0].Tags != nil || v.Labels != nil {
		t.Errorf("the original was modified: %+v", v)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
