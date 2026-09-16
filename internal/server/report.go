package server

import (
	"net/http"

	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/report"
)

// reportResponse is the report plus the two counts a widget needs.
//
// The counts are added here rather than in internal/report because `report
// --json` does not need them — a JSON consumer counts the arrays itself. A
// dashboard widget cannot: Homepage's customapi maps a field to a value and
// has no way to take the length of a list, so pointing it at needs_attention
// renders NaN. Checked by running one.
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
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if srv, ok := s.isRemoteRequest(r); ok {
		s.forwardRemote(w, srv, "report", "--json")
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
		s.forwardRemote(w, srv, "report", "--json")
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
