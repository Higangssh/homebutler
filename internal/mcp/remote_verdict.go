package mcp

import "encoding/json"

// failingVerdicts are the tools whose command prints its whole answer under
// --json and then exits non-zero because the answer is a failure. Locally
// the tool returns that answer; over SSH the exit used to turn it into an
// error, so a drill that failed reached the agent as "remote command failed"
// with no DrillResult to say which step broke.
//
// Each one says whether the answer it was handed really is the failure the
// exit reports. An answer that claims success beside a failing exit is not
// the verdict, so the error stands.
var failingVerdicts = map[string]func([]byte) bool{
	"backup_drill": func(out []byte) bool {
		var doc map[string]json.RawMessage
		if json.Unmarshal(out, &doc) != nil {
			return false
		}
		if _, all := doc["results"]; all {
			// backup drill --all: a DrillReport, whose passed is a count.
			var failed int
			return json.Unmarshal(doc["failed"], &failed) == nil && failed > 0
		}
		var passed bool
		return json.Unmarshal(doc["passed"], &passed) == nil && !passed
	},
	"notify_test": func(out []byte) bool {
		var results []struct {
			Sent *bool `json:"sent"`
		}
		if json.Unmarshal(out, &results) != nil {
			return false
		}
		for _, r := range results {
			if r.Sent != nil && !*r.Sent {
				return true
			}
		}
		return false
	},
}
