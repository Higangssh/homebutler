package cmd

import (
	"path/filepath"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/doctor"
)

func captureDoctor(t *testing.T) *[]doctor.Options {
	t.Helper()
	var got []doctor.Options
	saved := runDoctor
	t.Cleanup(func() { runDoctor = saved })
	runDoctor = func(_ *config.Config, _ doctor.CollectFuncs, opts doctor.Options) (*doctor.Result, error) {
		got = append(got, opts)
		return &doctor.Result{Status: doctor.SeverityPass, Findings: []doctor.Finding{}}, nil
	}
	return &got
}

// --no-save is what keeps doctor from writing its registry cache. Dropped
// on the way to doctor.Run, the flag would parse and do nothing.
func TestDoctorNoSaveReachesTheRun(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"--no-save"}, true},
		{nil, false},
	} {
		got := captureDoctor(t)
		savedPath, savedServer := cfgPath, serverName
		t.Cleanup(func() { cfgPath, serverName = savedPath, savedServer })
		cfgPath, serverName = filepath.Join(t.TempDir(), "none.yaml"), ""

		cmd := newDoctorCmd()
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if len(*got) != 1 || (*got)[0].NoSave != tc.want {
			t.Fatalf("args %v: doctor ran with %+v, want NoSave %v", tc.args, *got, tc.want)
		}
	}
}

// The same for this machine's line in --all.
func TestDoctorAllPassesNoSaveForThisMachine(t *testing.T) {
	for _, want := range []bool{true, false} {
		got := captureDoctor(t)
		cmd := newDoctorCmd()
		if want {
			if err := cmd.Flags().Set("no-save", "true"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := fanOuts["doctor"].local(&config.Config{}, cmd); err != nil {
			t.Fatal(err)
		}
		if len(*got) != 1 || (*got)[0].NoSave != want {
			t.Fatalf("--all ran doctor with %+v, want NoSave %v", *got, want)
		}
	}
}
