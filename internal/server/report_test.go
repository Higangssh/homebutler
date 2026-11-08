package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

// remoteReport is what `report --json` on the far side answers with, plus one
// field this binary does not know, standing in for a newer remote.
const remoteReport = `{"schema_version":1,"server_name":"remote1","snapshot_saved":false,` +
	`"needs_attention":[{"title":"disk"},{"title":"memory"}],` +
	`"notable_changes":[{"kind":"new"}],"from_a_newer_remote":"kept"}`

func TestRemoteReportComparesWithoutSaving(t *testing.T) {
	var got []string
	srv := testServerWithMockRemote(func(_ *config.ServerConfig, args ...string) ([]byte, error) {
		got = args
		return []byte(remoteReport), nil
	})
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/report?server=remote1", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Without --no-save every poll saved a snapshot on the remote and moved
	// the baseline the next comparison is measured from.
	if !slices.Contains(got, "--no-save") {
		t.Fatalf("GET /api/report?server= forwarded %q, which saves a snapshot on the remote", got)
	}
	assertRemoteSummary(t, w.Body.Bytes())
}

func TestRemoteSnapshotStillSaves(t *testing.T) {
	var got []string
	srv := testServerWithMockRemote(func(_ *config.ServerConfig, args ...string) ([]byte, error) {
		got = args
		return []byte(remoteReport), nil
	})
	srv.SetToken("secret")
	req := httptest.NewRequest("POST", "/api/report/snapshot?server=remote1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if slices.Contains(got, "--no-save") {
		t.Fatalf("POST /api/report/snapshot?server= forwarded %q, which saves nothing", got)
	}
	assertRemoteSummary(t, w.Body.Bytes())
}

func TestRemoteReportRejectsAnAnswerThatIsNotAReport(t *testing.T) {
	// null decodes into a map without error and leaves it nil, which is the
	// case that panicked rather than answering 502.
	for _, body := range []string{`[]`, "null\n"} {
		srv := testServerWithMockRemote(func(_ *config.ServerConfig, _ ...string) ([]byte, error) {
			return []byte(body), nil
		})
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/report?server=remote1", nil))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("remote answered %q: expected 502, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func assertRemoteSummary(t *testing.T, body []byte) {
	t.Helper()
	var doc struct {
		ServerName string `json:"server_name"`
		Newer      string `json:"from_a_newer_remote"`
		Summary    *struct {
			NeedsAttention int `json:"needs_attention"`
			NotableChanges int `json:"notable_changes"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if doc.Summary == nil {
		t.Fatalf("a remote report has no summary, so a widget reading summary.* shows nothing: %s", body)
	}
	if doc.Summary.NeedsAttention != 2 || doc.Summary.NotableChanges != 1 {
		t.Fatalf("summary = %+v, want needs_attention 2 and notable_changes 1", *doc.Summary)
	}
	if doc.ServerName != "remote1" || doc.Newer != "kept" {
		t.Fatalf("the remote's own fields did not pass through: %s", body)
	}
}
