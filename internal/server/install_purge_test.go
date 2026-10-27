package server

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Higangssh/homebutler/internal/install"
)

// The dashboard's purge route answered `data_preserved: false` and nothing
// else, so it could not say what `install purge` prints when a volume another
// container was using is still there.
func TestInstallPurgeRouteSaysWhichVolumesWereKeptOrUnchecked(t *testing.T) {
	orig := purgeApp
	t.Cleanup(func() { purgeApp = orig })

	purgeApp = func(string) (install.PurgeResult, error) {
		return install.PurgeResult{
			Removed:   []string{"gone"},
			Kept:      []string{"shared"},
			Unchecked: []string{"unknown"},
		}, nil
	}
	got := purgeRouteAnswer(t)
	if !reflect.DeepEqual(got["kept_volumes"], []any{"shared"}) {
		t.Errorf("kept_volumes = %#v, want [shared]", got["kept_volumes"])
	}
	if !reflect.DeepEqual(got["unchecked_volumes"], []any{"unknown"}) {
		t.Errorf("unchecked_volumes = %#v, want [unknown]", got["unchecked_volumes"])
	}
	if got["data_preserved"] != false {
		t.Errorf("data_preserved = %#v, want false", got["data_preserved"])
	}

	purgeApp = func(string) (install.PurgeResult, error) {
		return install.PurgeResult{Removed: []string{"gone"}}, nil
	}
	got = purgeRouteAnswer(t)
	for _, key := range []string{"kept_volumes", "unchecked_volumes"} {
		if _, ok := got[key]; ok {
			t.Errorf("a complete purge answered with %s = %#v", key, got[key])
		}
	}
}

func purgeRouteAnswer(t *testing.T) map[string]any {
	t.Helper()
	s := &Server{}
	req := httptest.NewRequest("POST", "/api/install/filebrowser/purge", nil)
	req.SetPathValue("app", "filebrowser")
	w := httptest.NewRecorder()
	s.handleInstallPurge(w, req)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return got
}
