package report

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
)

const (
	oldNginx = "sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85"
	newNginx = "sha256:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"
)

func withImages(s *Snapshot, ids map[string]string) *Snapshot {
	s.ImageIDs = ids
	return s
}

// nginxLine is the one change reported for the container named nginx.
func nginxLine(t *testing.T, r *Report) string {
	t.Helper()
	var lines []string
	for _, c := range r.NotableChanges {
		if c.Target == "nginx" {
			lines = append(lines, c.Text)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("want one line for nginx, got %q", lines)
	}
	return lines[0]
}

// The update most home servers get: docker compose pull && up -d. The
// container is recreated, docker ps says nginx:latest before and after, and
// the code underneath is different. A recreate is a replaced; the image IDs
// are what the detail adds.
func TestARecreateOntoANewerImageUnderTheSameTagNamesTheImage(t *testing.T) {
	prev := withImages(snapshotWith([]docker.Container{
		{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:latest", State: "running"},
	}, nil), map[string]string{"7d4a91f0aa11": oldNginx})
	curr := withImages(snapshotWith([]docker.Container{
		{ID: "91be0322bb22", Name: "nginx", Image: "nginx:latest", State: "running"},
	}, nil), map[string]string{"91be0322bb22": newNginx})

	got := nginxLine(t, buildReport(curr, prev))
	want := "replaced: nginx — recreated, 7d4a91f0aa11 → 91be0322bb22, image 605c77e624dd → 1a2b3c4d5e6f under nginx:latest"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// #58: recreated onto the image it already ran says only that it was
// recreated.
func TestARecreateOntoTheSameImageSaysNothingAboutTheImage(t *testing.T) {
	prev := withImages(snapshotWith([]docker.Container{
		{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:latest", State: "running"},
	}, nil), map[string]string{"7d4a91f0aa11": oldNginx})
	curr := withImages(snapshotWith([]docker.Container{
		{ID: "91be0322bb22", Name: "nginx", Image: "nginx:latest", State: "running"},
	}, nil), map[string]string{"91be0322bb22": oldNginx})

	got := nginxLine(t, buildReport(curr, prev))
	if want := "replaced: nginx — recreated, 7d4a91f0aa11 → 91be0322bb22"; got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// A tag that changed on the recreate is named by the tags, as before image
// IDs were recorded. The IDs differ too, and saying so as well would put the
// same fact in the line twice.
func TestARecreateOntoANewTagNamesTheTagsNotTheIDs(t *testing.T) {
	prev := withImages(snapshotWith([]docker.Container{
		{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:1.27", State: "running"},
	}, nil), map[string]string{"7d4a91f0aa11": oldNginx})
	curr := withImages(snapshotWith([]docker.Container{
		{ID: "91be0322bb22", Name: "nginx", Image: "nginx:1.28", State: "running"},
	}, nil), map[string]string{"91be0322bb22": newNginx})

	got := nginxLine(t, buildReport(curr, prev))
	if want := "replaced: nginx — recreated, 7d4a91f0aa11 → 91be0322bb22, nginx:1.27 → nginx:1.28"; got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// A report taken after the pull and before the recreate saw the container's
// image as a bare ID, because the tag had moved off it. Against that, the
// recreate is the same update as above, not a change from an ID to a tag.
func TestARecreateAfterAReportBetweenPullAndUpReadsAsTheSameUpdate(t *testing.T) {
	prev := withImages(snapshotWith([]docker.Container{
		{ID: "7d4a91f0aa11", Name: "nginx", Image: "605c77e624dd", State: "running"},
	}, nil), map[string]string{"7d4a91f0aa11": oldNginx})
	curr := withImages(snapshotWith([]docker.Container{
		{ID: "91be0322bb22", Name: "nginx", Image: "nginx:latest", State: "running"},
	}, nil), map[string]string{"91be0322bb22": newNginx})

	got := nginxLine(t, buildReport(curr, prev))
	want := "replaced: nginx — recreated, 7d4a91f0aa11 → 91be0322bb22, image 605c77e624dd → 1a2b3c4d5e6f under nginx:latest"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// An image ID missing on either side is not evidence of anything, so the line
// is the one written before image IDs were recorded.
func TestAMissingImageIDLeavesTheLineAsItWas(t *testing.T) {
	for name, ids := range map[string][2]map[string]string{
		"missing before": {nil, {"91be0322bb22": newNginx}},
		"missing now":    {{"7d4a91f0aa11": oldNginx}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			prev := withImages(snapshotWith([]docker.Container{
				{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:latest", State: "running"},
			}, nil), ids[0])
			curr := withImages(snapshotWith([]docker.Container{
				{ID: "91be0322bb22", Name: "nginx", Image: "nginx:latest", State: "running"},
			}, nil), ids[1])
			got := nginxLine(t, buildReport(curr, prev))
			if want := "replaced: nginx — recreated, 7d4a91f0aa11 → 91be0322bb22"; got != want {
				t.Errorf("\n got %s\nwant %s", got, want)
			}
		})
	}
}

// The ID has to survive the trip to disk: the comparison that matters is
// between this run and the snapshot the last run saved.
func TestTheImageIDIsSavedAndComparedOnTheNextRun(t *testing.T) {
	r := newReports(t)
	run := func(id, image string) *Report {
		t.Helper()
		r.at = r.at.Add(time.Hour)
		fns := fakeFuncs([]docker.Container{{ID: id, Name: "nginx", Image: "nginx:latest", State: "running"}}, nil)
		fns.ImageIDsFn = func(ids []string) (map[string]string, error) {
			if len(ids) != 1 || ids[0] != id {
				t.Errorf("asked for %q, want [%s]", ids, id)
			}
			return map[string]string{id: image}, nil
		}
		rep, err := Run(&config.Config{}, fns, Options{SnapshotDir: r.dir, Keep: DefaultKeep})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		return rep
	}

	run("7d4a91f0aa11", oldNginx)
	if got := r.latest().ImageIDs["7d4a91f0aa11"]; got != oldNginx {
		t.Fatalf("the snapshot did not keep the image ID: %q", got)
	}
	got := nginxLine(t, run("91be0322bb22", newNginx))
	if !strings.HasSuffix(got, "image 605c77e624dd → 1a2b3c4d5e6f under nginx:latest") {
		t.Errorf("the saved image ID was not compared against: %s", got)
	}
}

// report is the one caller that asks for image IDs. Without this the feature
// is tested everywhere except where it is switched on.
func TestReportAsksDockerForImageIDs(t *testing.T) {
	fns := DefaultCollectFuncs()
	if fns.ImageIDsFn == nil {
		t.Fatal("DefaultCollectFuncs does not read image IDs, so no snapshot records them")
	}
	if reflect.ValueOf(fns.ImageIDsFn).Pointer() != reflect.ValueOf(docker.ImageIDs).Pointer() {
		t.Error("DefaultCollectFuncs reads image IDs from something other than docker.ImageIDs")
	}
}

// Not reading image IDs is not Docker failing: the containers are still
// compared, by reference, and the warning says why.
func TestImageIDsThatCannotBeReadLeaveTheContainersCompared(t *testing.T) {
	fns := fakeFuncs([]docker.Container{{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:latest", State: "running"}}, nil)
	fns.ImageIDsFn = func([]string) (map[string]string, error) { return nil, errors.New("permission denied") }
	rep, err := Run(&config.Config{}, fns, Options{SnapshotDir: t.TempDir(), NoSave: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 0 {
		t.Errorf("image IDs are not a collector: %q", rep.Failed)
	}
	if rep.Running == nil || *rep.Running != 1 {
		t.Errorf("the containers were dropped with the image IDs: %v", rep.Running)
	}
	warned := false
	for _, w := range rep.Warnings {
		warned = warned || strings.Contains(w, "image IDs not read") && strings.HasSuffix(w, "permission denied")
	}
	if !warned {
		t.Errorf("no warning carrying the reason images were compared by tag only: %q", rep.Warnings)
	}
}

// A partial answer is kept: the containers it covers are compared by ID, and
// the warning is for the rest.
func TestAPartialImageIDAnswerIsKeptAndWarnedAbout(t *testing.T) {
	fns := fakeFuncs([]docker.Container{
		{ID: "7d4a91f0aa11", Name: "nginx", Image: "nginx:latest", State: "running"},
		{ID: "05fcf603364d", Name: "gone-now", Image: "alpine", State: "running"},
	}, nil)
	fns.ImageIDsFn = func([]string) (map[string]string, error) {
		return map[string]string{"7d4a91f0aa11": oldNginx}, errors.New("docker inspect answered for 1 of 2 containers, not 05fcf603364d")
	}
	dir := t.TempDir()
	rep, err := Run(&config.Config{}, fns, Options{SnapshotDir: dir, Keep: DefaultKeep})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) == 0 || !strings.Contains(strings.Join(rep.Warnings, "\n"), "not 05fcf603364d") {
		t.Errorf("the container left out was not named: %q", rep.Warnings)
	}
	snap, err := loadLatest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ImageIDs["7d4a91f0aa11"] != oldNginx {
		t.Errorf("the answer docker did give was dropped: %v", snap.ImageIDs)
	}
}

// Every key a snapshot writes is snake_case, as the frozen JSON is. The
// contract golden does not cover Snapshot, so it is checked here, including
// the types nested in it.
func TestEverySnapshotKeyIsSnakeCase(t *testing.T) {
	key := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(t2 reflect.Type, at string) {
		for t2.Kind() == reflect.Pointer || t2.Kind() == reflect.Slice || t2.Kind() == reflect.Map {
			t2 = t2.Elem()
		}
		if t2.Kind() != reflect.Struct || seen[t2] {
			return
		}
		seen[t2] = true
		for i := 0; i < t2.NumField(); i++ {
			f := t2.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.PkgPath != "" || name == "-" {
				continue
			}
			if !key.MatchString(name) {
				t.Errorf("%s.%s is written as %q, which is not snake_case", at, f.Name, name)
			}
			walk(f.Type, at+"."+name)
		}
	}
	walk(reflect.TypeOf(Snapshot{}), "snapshot")
	if f, ok := reflect.TypeOf(Snapshot{}).FieldByName("ImageIDs"); !ok || f.Tag.Get("json") != "image_ids,omitempty" {
		t.Errorf("the image IDs are not written as image_ids: %v", f.Tag)
	}
}

// Upgrading: the snapshot on disk was written before image IDs existed, and
// this run records them. A container recreated since is reported as it was
// before, with nothing said about an image the old snapshot did not record.
func TestAnOldSnapshotAgainstOneWithImageIDsComparesAsBefore(t *testing.T) {
	for version, path := range snapshotFixtures(t) {
		if strings.HasSuffix(version, "-docker-down") {
			continue
		}
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), data, 0o644); err != nil {
				t.Fatal(err)
			}
			prev, err := loadLatest(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(prev.ImageIDs) != 0 {
				t.Fatalf("the %s fixture already has image IDs", version)
			}

			// The same containers, each recreated onto a new image under its
			// old reference.
			now := make([]docker.Container, len(prev.Containers))
			for i, c := range prev.Containers {
				c.ID = "ffff" + c.ID[4:]
				now[i] = c
			}
			fns := fakeFuncs(now, nil)
			fns.ImageIDsFn = func(ids []string) (map[string]string, error) {
				out := map[string]string{}
				for _, id := range ids {
					out[id] = newNginx
				}
				return out, nil
			}
			rep, err := Run(&config.Config{}, fns, Options{SnapshotDir: dir, NoSave: true})
			if err != nil {
				t.Fatal(err)
			}
			if rep.IsBaseline {
				t.Fatal("the old snapshot was not compared against")
			}
			replaced := 0
			for _, c := range rep.NotableChanges {
				if !strings.HasPrefix(c.Target, "hb-fixture-") {
					continue
				}
				if c.Kind != kindReplaced || strings.Contains(c.Detail, "image") {
					t.Errorf("want a plain replaced against a snapshot without image IDs, got %s", c.Text)
				}
				replaced++
			}
			if replaced != len(now) {
				t.Errorf("want %d replaced lines, got %d: %+v", len(now), replaced, rep.NotableChanges)
			}
		})
	}
}
