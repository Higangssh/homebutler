package mcp

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/install"
)

// The CLI has said which volumes a purge kept or could not check since #326.
// install_purge answered `{status: purged, app, data_preserved: false}` either
// way, so an agent was told nothing was left while a volume still existed.
func TestInstallPurgeSaysWhichVolumesWereKeptOrUnchecked(t *testing.T) {
	orig := purgeApp
	t.Cleanup(func() { purgeApp = orig })

	s := NewServer(&config.Config{}, "test")

	purgeApp = func(string) (install.PurgeResult, error) {
		return install.PurgeResult{
			Removed:   []string{"gone"},
			Kept:      []string{"shared"},
			Unchecked: []string{"unknown"},
		}, nil
	}
	got := purgeAnswer(t, s)
	if !reflect.DeepEqual(got["kept_volumes"], []any{"shared"}) {
		t.Errorf("kept_volumes = %#v, want [shared]", got["kept_volumes"])
	}
	if !reflect.DeepEqual(got["unchecked_volumes"], []any{"unknown"}) {
		t.Errorf("unchecked_volumes = %#v, want [unknown]", got["unchecked_volumes"])
	}
	if got["data_preserved"] != false {
		t.Errorf("data_preserved = %#v, want false", got["data_preserved"])
	}

	// A complete purge keeps the shape it had: the new keys are absent rather
	// than empty, so a caller that never saw them sees nothing new.
	purgeApp = func(string) (install.PurgeResult, error) {
		return install.PurgeResult{Removed: []string{"gone"}}, nil
	}
	got = purgeAnswer(t, s)
	for _, key := range []string{"kept_volumes", "unchecked_volumes"} {
		if _, ok := got[key]; ok {
			t.Errorf("a complete purge answered with %s = %#v", key, got[key])
		}
	}
}

func purgeAnswer(t *testing.T, s *Server) map[string]any {
	t.Helper()
	result, err := s.executeTool("install_purge", map[string]any{"app": "filebrowser"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
