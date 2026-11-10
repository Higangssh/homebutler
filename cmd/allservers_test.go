package cmd

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/spf13/cobra"
)

// fakeServers stands in for one local and one SSH server. The SSH one answers
// as a real homebutler does: JSON when asked for it, text otherwise.
type fakeServers struct {
	mu         sync.Mutex
	remoteArgs [][]string
	local      string
	remoteJSON string
	remoteText string
	remoteErr  error
}

func (f *fakeServers) install(t *testing.T) {
	t.Helper()
	savedRemote, savedLocal := remoteRun, localRun
	t.Cleanup(func() { remoteRun, localRun = savedRemote, savedLocal })
	remoteRun = func(_ *config.ServerConfig, args ...string) ([]byte, error) {
		f.mu.Lock()
		f.remoteArgs = append(f.remoteArgs, args)
		f.mu.Unlock()
		if f.remoteErr != nil {
			return nil, f.remoteErr
		}
		if slices.Contains(args, "--json") {
			return []byte(f.remoteJSON), nil
		}
		return []byte(f.remoteText), nil
	}
	localRun = func(fanOut, *config.Config, *cobra.Command) ([]byte, error) {
		return []byte(f.local), nil
	}
}

func twoServers() *config.Config {
	return &config.Config{Servers: []config.ServerConfig{
		{Name: "here", Local: true},
		{Name: "pi", Host: "192.0.2.4"},
	}}
}

func runFanOut(t *testing.T, args []string, jsonOut bool) string {
	t.Helper()
	var buf bytes.Buffer
	if err := fanOutAll(&buf, twoServers(), args, jsonOut); err != nil {
		t.Fatalf("fanOutAll(%v): %v", args, err)
	}
	return buf.String()
}

const statusJSON = `{"schema_version":1,"hostname":"pi","os":"linux","arch":"arm64","uptime":"11h 51m",
	"cpu":{"usage_percent":12,"cores":4},"memory":{"total_gb":8,"used_gb":0.4,"usage_percent":5},
	"disks":[{"mount":"/","total_gb":58,"used_gb":10,"usage_percent":18}],"time":"2026-11-09T10:00:00Z"}`

// Without --json the SSH server was asked for text, and every SSH server in
// the human output was "(parse error)".
func TestFanOutAsksTheRemoteForJSONWithoutJSONFlag(t *testing.T) {
	f := &fakeServers{local: statusJSON, remoteJSON: statusJSON, remoteText: "🖥  pi (linux/arm64)\n   Uptime: 11h 51m\n"}
	f.install(t)

	got := runFanOut(t, []string{"status"}, false)

	if strings.Contains(got, "parse error") || strings.Contains(got, "cannot read") {
		t.Fatalf("the SSH server's answer was not read:\n%s", got)
	}
	want := "📡 pi           CPU   12% | Mem    5% | Disk   18% | Up 11h 51m\n"
	if !strings.Contains(got, want) {
		t.Errorf("want line %q in:\n%s", want, got)
	}
	if len(f.remoteArgs) != 1 || !slices.Equal(f.remoteArgs[0], []string{"status", "--json"}) {
		t.Errorf("remote was asked %v, want [status --json]", f.remoteArgs)
	}
}

// --json is sent once whether or not the user typed it, and wherever.
func TestFanOutSendsJSONExactlyOnce(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--json"},
		{"--json", "status"},
		{"status", "--json=true"},
	} {
		f := &fakeServers{local: statusJSON, remoteJSON: statusJSON}
		f.install(t)
		runFanOut(t, args, true)
		if !slices.Equal(f.remoteArgs[0], []string{"status", "--json"}) {
			t.Errorf("%v: remote was asked %v, want [status --json]", args, f.remoteArgs[0])
		}
	}
}

// alerts was printed with the status formatter, and its shape has no
// cpu.usage_percent, so every machine read 0% CPU, memory and disk.
func TestFanOutAlertsLineIsReadFromAlertsAnswer(t *testing.T) {
	alertsJSON := `{"cpu":{"status":"ok","current":41,"threshold":90},
		"memory":{"status":"warning","current":88,"threshold":85},
		"disks":[{"mount":"/","status":"ok","current":18,"threshold":90},
		         {"mount":"/mnt/data","status":"critical","current":97,"threshold":90}]}`
	f := &fakeServers{local: alertsJSON, remoteJSON: alertsJSON}
	f.install(t)

	got := runFanOut(t, []string{"alerts"}, false)

	want := "📡 pi           CRITICAL | CPU   41% | Mem   88% warning | Disk   97% /mnt/data critical (fullest of 2)\n"
	if !strings.Contains(got, want) {
		t.Errorf("want line %q in:\n%s", want, got)
	}
	if strings.Contains(got, "  0%") || strings.Contains(got, "Up") {
		t.Errorf("alerts printed as status:\n%s", got)
	}
}

// A field the answer did not carry is a dash, and an answer with none of the
// command's fields is said to be unreadable — neither is a zero.
func TestFanOutNeverPrintsZeroForAMissingField(t *testing.T) {
	line, err := summarizeStatus([]byte(`{"uptime":"3d","memory":{"usage_percent":40}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "CPU     - | Mem   40% | Disk     - | Up 3d"; line != want {
		t.Errorf("got %q, want %q", line, want)
	}

	line, err = summarizeAlerts([]byte(`{"memory":{"status":"ok","current":40,"threshold":85}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "OK       | CPU     - | Mem   40% | Disk     -"; line != want {
		t.Errorf("got %q, want %q", line, want)
	}

	for name, f := range fanOuts {
		for _, answer := range []string{`{}`, `"text"`, `{"unrelated":1}`} {
			line, err := f.summary([]byte(answer))
			if err == nil {
				t.Errorf("%s summarised %s as %q; want it refused as unreadable", name, answer, line)
			}
		}
	}
}

func TestFanOutUnreadableAnswerIsSaidSo(t *testing.T) {
	f := &fakeServers{local: statusJSON, remoteJSON: `{"something":"else"}`}
	f.install(t)

	got := runFanOut(t, []string{"status"}, false)
	if !strings.Contains(got, "⚠️  pi           answered with something this version cannot read") {
		t.Errorf("unreadable answer not reported:\n%s", got)
	}
}

func TestFanOutSummaries(t *testing.T) {
	cases := []struct {
		command string
		answer  string
		want    string
	}{
		{"docker list", `[{"name":"a","state":"running"},{"name":"b","state":"exited"},{"name":"c","state":"running"}]`,
			"3 containers | 2 running | 1 not running"},
		{"docker list", `[{"name":"a","state":"exited"}]`, "1 container | 0 running | 1 not running"},
		{"docker list", `[]`, "no containers"},
		{"docker list", `null`, "no containers"},
		{"docker stats", `[{"name":"db","cpu_percent":"1.50%"},{"name":"web","cpu_percent":"12.25%"}]`,
			"2 running containers | busiest web at 12.2% CPU"},
		{"docker stats", `[]`, "no running containers"},
		{"ports", `[{"protocol":"tcp","address":"0.0.0.0","port":"22"},{"protocol":"tcp","address":"[::]","port":"22"},{"protocol":"udp","address":"0.0.0.0","port":"53"}]`,
			"2 open ports (tcp 1, udp 1)"},
		{"ports", `[]`, "no open ports"},
		{"doctor", `{"schema_version":1,"status":"warn","summary":{"pass":9,"warn":4,"fail":0},"findings":[]}`,
			"WARN | warn 4 | fail 0"},
		{"processes", `{"processes":[{"pid":1,"name":"sshd","cpu":8.3,"mem":0.1,"rss":9000}],"total":42}`,
			"42 processes | top sshd (CPU 8.3%, Mem 0.1%) | 0 zombies"},
		{"processes", `{"processes":[],"total":1,"zombies":[{"pid":9,"name":"x"}]}`,
			"1 process | 1 zombie"},
		{"report", `{"schema_version":1,"is_baseline":false,"snapshot_saved":true,"needs_attention":[{}],"notable_changes":[{},{}],"compared_to":"2026-11-08T14:40:41Z"}`,
			"1 need attention | 2 notable changes | since 2026-11-08T14:40:41Z | snapshot saved"},
		{"report", `{"schema_version":1,"is_baseline":true,"snapshot_saved":false,"needs_attention":[],"notable_changes":[{}]}`,
			"0 need attention | 1 notable change | first snapshot, nothing to compare"},
		{"backup list", `[{"name":"old.tar.gz","size":"1 MB","created_at":"2026-10-01T10:00:00+09:00"},{"name":"new.tar.gz","size":"2 MB","created_at":"2026-10-17T15:23:46+09:00"}]`,
			"2 backups | newest new.tar.gz (2026-10-17T15:23:46+09:00, 2 MB)"},
		{"backup list", `[]`, "no backups"},
		{"network scan", `[{"ip":"192.0.2.1","status":"up"}]`, "1 device found"},
	}
	for _, c := range cases {
		f, ok := fanOuts[c.command]
		if !ok {
			t.Fatalf("%s is not a fan-out command", c.command)
		}
		got, err := f.summary([]byte(c.answer))
		if err != nil {
			t.Errorf("%s %s: %v", c.command, c.answer, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s %s:\n got %q\nwant %q", c.command, c.answer, got, c.want)
		}
	}
}

// Commands that change the machine, or read one named thing on one server,
// are refused before any server is asked. docker restart used to go to every
// SSH server while the local one refused it.
func TestFanOutRefusesCommandsWithoutASummary(t *testing.T) {
	for _, args := range [][]string{
		{"docker", "restart", "nginx"},
		{"docker", "stop", "nginx"},
		{"docker", "logs", "nginx"},
		{"docker", "top", "nginx"},
		{"docker", "inspect", "nginx"},
		{"wake", "aa:bb:cc:dd:ee:ff"},
		{"backup"},
		{"restore", "archive.tar.gz"},
	} {
		f := &fakeServers{local: `{}`, remoteJSON: `{}`}
		f.install(t)
		var buf bytes.Buffer
		err := fanOutAll(&buf, twoServers(), args, false)
		if err == nil || !strings.Contains(err.Error(), "does not run with --all") {
			t.Errorf("%v: err = %v, want a refusal", args, err)
		}
		if len(f.remoteArgs) != 0 {
			t.Errorf("%v: a server was asked %v before the refusal", args, f.remoteArgs)
		}
	}
}

func TestFanOutRefusesAlertsWatch(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"alerts"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("watch", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("watch", "false") })

	f := &fakeServers{local: `{}`, remoteJSON: `{}`}
	f.install(t)
	err = fanOutAll(&bytes.Buffer{}, twoServers(), []string{"alerts", "--watch"}, false)
	if err == nil || !strings.Contains(err.Error(), "alerts --watch does not run with --all") {
		t.Errorf("err = %v, want the --watch refusal", err)
	}
	if len(f.remoteArgs) != 0 {
		t.Errorf("a server was asked %v before the refusal", f.remoteArgs)
	}
}

// Every key names a real command, aliases included, so a typo cannot quietly
// turn a supported command into a refused one.
func TestFanOutKeysAreCommands(t *testing.T) {
	for name := range fanOuts {
		f, cmd, err := fanOutFor(strings.Fields(name))
		if err != nil || cmd == nil || f.summary == nil || f.local == nil {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	for _, alias := range [][]string{{"docker", "ls"}, {"ps"}} {
		if _, _, err := fanOutFor(alias); err != nil {
			t.Errorf("%v: %v", alias, err)
		}
	}
}

// --json --all is what scripts read, and it is the same envelope it was: one
// entry per server, data passed through, error as a string.
func TestFanOutJSONEnvelopeIsUnchanged(t *testing.T) {
	f := &fakeServers{local: `{"total":1,"processes":[]}`, remoteErr: errors.New("[pi] connection refused")}
	f.install(t)

	var buf bytes.Buffer
	if err := fanOutAll(&buf, twoServers(), []string{"processes", "--json"}, true); err == nil {
		t.Error("pi could not be asked and the run passed")
	}
	got := buf.String()
	want := `[
  {
    "server": "here",
    "data": {
      "total": 1,
      "processes": []
    }
  },
  {
    "server": "pi",
    "error": "[pi] connection refused"
  }
]
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// --all=true sent on made the remote fan out again from its own config, and
// --server=rpi5 made it look for a server called itself.
func TestFanOutStripsFlagsWrittenWithEquals(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--all=true"},
		{"status", "--server=rpi5"},
		{"status", "--server", "rpi5"},
		{"status", "--config=/tmp/x.yaml"},
	} {
		f := &fakeServers{local: statusJSON, remoteJSON: statusJSON}
		f.install(t)
		// root.go takes --config off before the fan-out; do the same.
		runFanOut(t, filterFlags(args, "--server", "--all", "--config"), false)
		if !slices.Equal(f.remoteArgs[0], []string{"status", "--json"}) {
			t.Errorf("%v: remote was asked %v, want [status --json]", args, f.remoteArgs[0])
		}
	}
	// And inside the fan-out itself, without root.go's pass.
	f := &fakeServers{local: statusJSON, remoteJSON: statusJSON}
	f.install(t)
	runFanOut(t, []string{"status", "--all=true", "--server=rpi5"}, false)
	if !slices.Equal(f.remoteArgs[0], []string{"status", "--json"}) {
		t.Errorf("remote was asked %v, want [status --json]", f.remoteArgs[0])
	}
}

// doctor --strict exits non-zero on a warning, so sent to a server it turned
// that server's line into "remote command failed". It is applied across the
// answers instead.
func TestFanOutDoctorStrictIsOneVerdict(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"doctor"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("strict", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("strict", "false") })

	pass := `{"schema_version":1,"status":"pass","summary":{"pass":9,"warn":0,"fail":0},"findings":[]}`
	warn := `{"schema_version":1,"status":"warn","summary":{"pass":8,"warn":1,"fail":0},"findings":[]}`

	f := &fakeServers{local: pass, remoteJSON: warn}
	f.install(t)
	var buf bytes.Buffer
	err = fanOutAll(&buf, twoServers(), []string{"doctor", "--strict"}, false)
	if !slices.Equal(f.remoteArgs[0], []string{"doctor", "--json"}) {
		t.Errorf("remote was asked %v, want [doctor --json]", f.remoteArgs[0])
	}
	if !strings.Contains(buf.String(), "🩺 pi           WARN | warn 1 | fail 0") {
		t.Errorf("the warning server's line is missing:\n%s", buf.String())
	}
	if err == nil || !strings.Contains(err.Error(), "1 of 2 servers: pi") {
		t.Errorf("err = %v, want the strict verdict naming pi", err)
	}

	f = &fakeServers{local: pass, remoteJSON: pass}
	f.install(t)
	if err := fanOutAll(&bytes.Buffer{}, twoServers(), []string{"doctor", "--strict"}, true); err != nil {
		t.Errorf("every server passed, err = %v", err)
	}

	f = &fakeServers{local: pass, remoteErr: errors.New("[pi] connection refused")}
	f.install(t)
	err = fanOutAll(&bytes.Buffer{}, twoServers(), []string{"doctor", "--strict"}, true)
	// One verdict, the strict one, not a second line for the same server.
	if err == nil || err.Error() != "doctor did not pass on 1 of 2 servers: pi" {
		t.Errorf("err = %v, want only the strict verdict naming pi", err)
	}
}

// status --all printed ❌ for a server it could not reach and exited 0, so
// `homebutler status --all && …` went on as if every server had answered.
func TestFanOutExitsNonZeroWhenAServerFails(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		f := &fakeServers{local: statusJSON, remoteErr: errors.New("[pi] connection refused")}
		f.install(t)
		var buf bytes.Buffer
		err := fanOutAll(&buf, twoServers(), []string{"status"}, jsonOut)
		if err == nil {
			t.Fatalf("json=%v: pi failed and status --all passed", jsonOut)
		}
		if got, want := err.Error(), "status failed on 1 of 2 servers: pi"; got != want {
			t.Errorf("json=%v: error = %q, want %q", jsonOut, got, want)
		}
		// Every server's answer is still printed before the verdict.
		out := buf.String()
		if jsonOut {
			if !strings.Contains(out, `"server": "here"`) || !strings.Contains(out, `"error": "[pi] connection refused"`) {
				t.Errorf("json output is missing a server:\n%s", out)
			}
		} else if !strings.Contains(out, "📡 here") || !strings.Contains(out, "❌ pi") {
			t.Errorf("human output is missing a server:\n%s", out)
		}
	}
}

// A server that answered with JSON this version cannot summarise did answer,
// and its JSON is passed through whole, so it is not a failure: only the one
// that could not be asked is named.
func TestFanOutUnreadableAnswerIsNotAFailure(t *testing.T) {
	f := &fakeServers{local: `{"from_a_newer_version":true}`, remoteErr: errors.New("[pi] connection refused")}
	f.install(t)
	var buf bytes.Buffer
	err := fanOutAll(&buf, twoServers(), []string{"status"}, false)
	if !strings.Contains(buf.String(), "⚠️  here") {
		t.Fatalf("want here's line to say it could not be read:\n%s", buf.String())
	}
	if err == nil || err.Error() != "status failed on 1 of 2 servers: pi" {
		t.Errorf("err = %v, want only pi named", err)
	}

	f = &fakeServers{local: `{"from_a_newer_version":true}`, remoteJSON: statusJSON}
	f.install(t)
	if err := fanOutAll(&bytes.Buffer{}, twoServers(), []string{"status"}, false); err != nil {
		t.Errorf("an unreadable answer failed the run: %v", err)
	}
}

// doctor without --strict has no verdict of its own, so a server it could not
// ask still fails the run.
func TestFanOutDoctorWithoutStrictStillFailsAnUnreachableServer(t *testing.T) {
	warn := `{"schema_version":1,"status":"warn","summary":{"pass":8,"warn":1,"fail":0},"findings":[]}`
	f := &fakeServers{local: warn, remoteErr: errors.New("[pi] connection refused")}
	f.install(t)
	err := fanOutAll(&bytes.Buffer{}, twoServers(), []string{"doctor"}, false)
	if err == nil || err.Error() != "doctor failed on 1 of 2 servers: pi" {
		t.Errorf("err = %v, want the fan-out verdict naming pi", err)
	}
}
