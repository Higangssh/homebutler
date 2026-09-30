package watch

import (
	"slices"
	"testing"
)

// The words are declared in one place and written in several, so this walks
// what Analyze can actually produce: every exit code it names, one it does
// not, OOMKilled, and each log pattern alone.
func TestAnalyzeOnlySaysWordsTheVocabularyDeclares(t *testing.T) {
	inputs := []CrashInfo{{OOMKilled: true, ExitCode: 137}}
	for _, code := range []int{0, 1, 2, 137, 139, 143} {
		inputs = append(inputs, CrashInfo{ExitCode: code})
	}
	for _, p := range logPatterns {
		inputs = append(inputs, CrashInfo{ExitCode: 2, ErrorLog: p.name})
	}
	for _, in := range inputs {
		got := Analyze(in)
		if !slices.Contains(CrashCategories(), got.Category) {
			t.Errorf("%+v classified as %q, which CrashCategories does not declare", in, got.Category)
		}
		if !slices.Contains(CrashConfidences(), got.Confidence) {
			t.Errorf("%+v given confidence %q, which CrashConfidences does not declare", in, got.Confidence)
		}
	}
}
