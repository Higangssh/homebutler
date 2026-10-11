package cmd

import (
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/install"
)

// "Completely removed" is only said when it is true.
func TestPurgeSummarySaysWhatWasNotRemoved(t *testing.T) {
	complete := purgeSummary(install.PurgeResult{Removed: []string{"aed3"}})
	if complete != "✅ Completely removed\n   removed volume aed3\n" {
		t.Errorf("complete purge read as %q", complete)
	}

	partial := purgeSummary(install.PurgeResult{
		Removed:   []string{"aed3"},
		Kept:      []string{"shared"},
		Unchecked: []string{"b1c9"},
	})
	want := "⚠️  Removed, except:\n" +
		"   kept volume shared (still exists)\n" +
		"   volume b1c9 (could not check whether it was removed)\n" +
		"   removed volume aed3\n"
	if partial != want {
		t.Errorf("partial purge read as\n%s\nwant\n%s", partial, want)
	}
	if strings.Contains(partial, "Completely") {
		t.Error("a purge that left a volume must not say it removed everything")
	}

	if got := purgeSummary(install.PurgeResult{}); got != "✅ Completely removed\n" {
		t.Errorf("an app with no volumes read as %q", got)
	}
}
