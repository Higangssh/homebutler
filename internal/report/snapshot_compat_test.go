package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
)

// Each directory under testdata/snapshots holds a snapshot written by that
// release's own binary, on a real machine with real containers. The README
// there says how they were made and what was changed afterwards.
//
// The snapshot format changed three times — v0.18.0, v0.22.0 (#74) and
// v0.26.0 (#129) — and v0.39.0 is the last release before 1.0. Somebody
// upgrading from any of them has one of these on disk, and the next report
// is compared against it or the history quietly starts again.
func snapshotFixtures(t *testing.T) map[string]string {
	t.Helper()
	dirs, err := filepath.Glob("testdata/snapshots/v*")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no snapshot fixtures found: %v", err)
	}
	out := map[string]string{}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "snapshot_*.json"))
		if err != nil || len(files) != 1 {
			t.Fatalf("%s: want exactly one snapshot, found %d", dir, len(files))
		}
		out[filepath.Base(dir)] = files[0]
	}
	return out
}

// The containers every fixture was taken with: two running, one stopped.
var fixtureContainers = []docker.Container{
	{Name: "hb-fixture-a", Image: "alpine", State: "running"},
	{Name: "hb-fixture-b", Image: "nginx:alpine", State: "running"},
	{Name: "hb-fixture-c", Image: "alpine", State: "exited"},
}

func TestASnapshotEveryReleaseWroteIsStillComparedAgainst(t *testing.T) {
	for version, path := range snapshotFixtures(t) {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), data, 0o644); err != nil {
				t.Fatal(err)
			}
			var written struct {
				Timestamp string `json:"timestamp"`
			}
			if err := json.Unmarshal(data, &written); err != nil {
				t.Fatal(err)
			}

			rep, err := Run(&config.Config{}, fakeFuncs(fixtureContainers, nil), Options{SnapshotDir: dir, NoSave: true})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if rep.IsBaseline || rep.ComparedTo != written.Timestamp {
				t.Fatalf("the %s snapshot was not compared against: baseline=%v compared_to=%q, want %q", version, rep.IsBaseline, rep.ComparedTo, written.Timestamp)
			}
			for _, w := range rep.Warnings {
				if strings.Contains(w, "could not be read") {
					t.Errorf("warned about a snapshot it read: %s", w)
				}
			}

			// The containers were read, not just the timestamp: the same
			// three are running now, so none of them is gone or new.
			for _, c := range rep.NotableChanges {
				if (c.Kind == kindGone || c.Kind == kindNew) && strings.HasPrefix(c.Target, "hb-fixture-") {
					t.Errorf("%s reported as %s; the snapshot's containers were not decoded", c.Target, c.Kind)
				}
			}
		})
	}
}

// Decoding succeeds when a key is renamed; the old key is dropped and the
// value with it. So every key an old release wrote has to still be a key the
// current Snapshot reads, or the comparison runs against a zero value and
// reports it as a change.
func TestEveryKeyAnOldSnapshotHoldsIsStillRead(t *testing.T) {
	for version, path := range snapshotFixtures(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		var lost []string
		unreadKeys(raw, reflect.TypeOf(Snapshot{}), "", &lost)
		sort.Strings(lost)
		for _, key := range lost {
			t.Errorf("%s wrote %s, and the current Snapshot does not read it", version, key)
		}
	}
}

func unreadKeys(v any, t reflect.Type, at string, lost *[]string) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	switch v := v.(type) {
	case []any:
		for _, item := range v {
			unreadKeys(item, t, at+"[]", lost)
		}
	case map[string]any:
		if t.Kind() != reflect.Struct {
			return
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.PkgPath != "" || name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			fields[name] = f.Type
		}
		for key, child := range v {
			ft, ok := fields[key]
			if !ok {
				*lost = append(*lost, at+"."+key)
				continue
			}
			unreadKeys(child, ft, at+"."+key, lost)
		}
	}
}

// A first run has no snapshot and says nothing about it: that is expected.
func TestAFirstRunIsABaselineWithoutAWarning(t *testing.T) {
	rep, err := Run(&config.Config{}, fakeFuncs(nil, nil), Options{SnapshotDir: t.TempDir(), NoSave: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.IsBaseline {
		t.Fatal("a first run must be a baseline")
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("a first run warned: %q", rep.Warnings)
	}
	if len(rep.NotableChanges) == 0 || !strings.HasPrefix(rep.NotableChanges[0].Text, "First inspection") {
		t.Errorf("a first run should say it is the first: %+v", rep.NotableChanges)
	}
}

// A snapshot that exists and cannot be read is not a first run. The report
// still goes out as a baseline, and says which file and why, because
// otherwise the history ends with nothing on the screen.
func TestAnUnreadableSnapshotIsABaselineThatSaysSo(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "snapshot_20260101T000000Z.json")
	if err := os.WriteFile(broken, []byte(`{"timestamp": 20260101}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(&config.Config{}, fakeFuncs(nil, nil), Options{SnapshotDir: dir, NoSave: true})
	if err != nil {
		t.Fatalf("an unreadable snapshot must not stop the report: %v", err)
	}
	if !rep.IsBaseline {
		t.Fatal("nothing could be compared, so this is a baseline")
	}
	var warned string
	for _, w := range rep.Warnings {
		if strings.Contains(w, "could not be read") {
			warned = w
		}
	}
	if warned == "" {
		t.Fatalf("no warning about the unreadable snapshot: %q", rep.Warnings)
	}
	if !strings.Contains(warned, broken) {
		t.Errorf("the warning does not name the file: %s", warned)
	}
	if len(rep.NotableChanges) == 0 || strings.HasPrefix(rep.NotableChanges[0].Text, "First inspection") {
		t.Errorf("an unreadable history is not a first inspection: %+v", rep.NotableChanges)
	}
}
