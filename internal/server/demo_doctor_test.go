package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/doctor"
)

// The demo doctor said WARN beside a failing finding and counted nine passes
// it did not list. Neither is a value the product can produce: overallStatus
// returns fail whenever fail is above zero, and the summary is counted from
// the findings rather than written next to them.
//
// It matters more here than in most places. This is the screen the site's
// screenshot is taken from, so a number that cannot happen is a number a lot
// of people see — and the last one that got out was a mount growing from
// 1.6 GB to 1.7 TB.
//
// The shape checks cannot catch this: `"status": "warn"` is a string where a
// string belongs. Only the relationship between the values is wrong.
func TestDemoDoctorSummaryMatchesItsFindings(t *testing.T) {
	s := New(&config.Config{}, "127.0.0.1", 8080, true)

	rec := httptest.NewRecorder()
	s.demoDoctor(rec, httptest.NewRequest(http.MethodGet, "/api/doctor", nil))

	var got struct {
		Status   string         `json:"status"`
		Summary  doctor.Summary `json:"summary"`
		Findings []struct {
			Severity string `json:"severity"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode demo doctor: %v", err)
	}

	counted := doctor.Summary{}
	for _, f := range got.Findings {
		switch f.Severity {
		case doctor.SeverityPass:
			counted.Pass++
		case doctor.SeverityWarn:
			counted.Warn++
		case doctor.SeverityFail:
			counted.Fail++
		default:
			t.Errorf("finding carries severity %q, which doctor does not emit", f.Severity)
		}
	}

	if counted != got.Summary {
		t.Errorf("the summary says %+v and the findings are %+v — a caller counting either one gets a different machine", got.Summary, counted)
	}

	// The status is not an opinion: it follows from the counts.
	want := doctor.SeverityPass
	switch {
	case counted.Fail > 0:
		want = doctor.SeverityFail
	case counted.Warn > 0:
		want = doctor.SeverityWarn
	}
	if got.Status != want {
		t.Errorf("status is %q with %d failing and %d to look at; the product would say %q", got.Status, counted.Fail, counted.Warn, want)
	}
}
