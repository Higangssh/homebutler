package server

import (
	"encoding/json"
	"net/http"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/report"
)

// reportResponse is the report plus the two counts a widget needs.
//
// The counts are added here rather than in internal/report because `report
// --json` does not need them — a JSON consumer counts the arrays itself. A
// dashboard widget maps a field to a value: Homepage's customapi pointed at
// needs_attention with `format: number` renders NaN. Its `format: size` can
// count a list, but a field that already holds the number keeps the snippet
// in docs/web-dashboard.md to one obvious mapping per value. Checked by
// running Homepage 2.7.0.
type reportResponse struct {
	*report.Report
	Summary reportSummary `json:"summary"`
}

type reportSummary struct {
	NeedsAttention int `json:"needs_attention"`
	NotableChanges int `json:"notable_changes"`
}

func withSummary(r *report.Report) reportResponse {
	return reportResponse{
		Report: r,
		Summary: reportSummary{
			NeedsAttention: len(r.NeedsAttention),
			NotableChanges: len(r.NotableChanges),
		},
	}
}

// handleReport compares the machine against the last saved snapshot without
// saving one.
//
// A dashboard polling this every fifteen seconds would otherwise take thirty
// snapshots in eight minutes and prune the baseline somebody wanted to compare
// against — so looking is looking, and saving is a separate request that costs
// a token.
//
// The same holds for a remote one, so the forwarded command carries --no-save.
// Every remote that has `report` understands it — both arrived in 0.18.0 — and
// one older than that has no `report` at all, which remote.Run already explains
// by naming the version on the far side.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemoteReport(w, srv, "report", "--json", "--no-save")
		return
	}

	result, err := report.Run(s.config(), report.DefaultCollectFuncs(), report.Options{NoSave: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, withSummary(result))
}

// handleReportSnapshot saves one, which is what makes the next comparison
// about the time since now.
func (s *Server) handleReportSnapshot(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemoteReport(w, srv, "report", "--json")
		return
	}

	// Keep matters here: zero means "keep one", so saving would delete the
	// snapshot this save is meant to be compared against next time.
	result, err := report.Run(s.config(), report.DefaultCollectFuncs(), report.Options{Keep: report.DefaultKeep})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, withSummary(result))
}

// forwardRemoteReport answers with the remote's report and the summary the
// local path adds, so a widget pointed at summary.* reads the same fields
// whichever server it names.
//
// The remote's answer is extended rather than decoded into report.Report and
// written again: a remote newer than this binary may send a field this one does
// not know, and a round trip through the struct would drop it.
func (s *Server) forwardRemoteReport(w http.ResponseWriter, srv *config.ServerConfig, args ...string) {
	out, err := s.remoteRunner(srv, args...)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	body, err := withRemoteSummary(out)
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid response from remote server")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func withRemoteSummary(out []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	var lists struct {
		NeedsAttention []json.RawMessage `json:"needs_attention"`
		NotableChanges []json.RawMessage `json:"notable_changes"`
	}
	if err := json.Unmarshal(out, &lists); err != nil {
		return nil, err
	}
	summary, err := json.Marshal(reportSummary{
		NeedsAttention: len(lists.NeedsAttention),
		NotableChanges: len(lists.NotableChanges),
	})
	if err != nil {
		return nil, err
	}
	doc["summary"] = summary
	return json.Marshal(doc)
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "doctor", "--json")
		return
	}

	result, err := doctor.Run(s.config(), doctor.DefaultCollectFuncs(), doctor.Options{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}
