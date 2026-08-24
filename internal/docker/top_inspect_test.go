package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustReadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

// The linux fixture is a verbatim capture of `docker top` (ps -ef layout:
// UID first, CMD last, command containing spaces). It pins the parsing this
// issue depends on rather than trusting whatever ps the test host happens to
// run — the same reasoning as the inventory port fixtures.
func TestParseDockerTopLinuxFixture(t *testing.T) {
	processes := parseDockerTop(mustReadFixture(t, "top-linux.txt"))

	if len(processes) != 2 {
		t.Fatalf("expected 2 processes from top-linux fixture, got %d", len(processes))
	}
	first := processes[0]
	if first.User != "lenny" {
		t.Errorf("user = %q, want lenny", first.User)
	}
	if first.PID != "2623" {
		t.Errorf("pid = %q, want 2623", first.PID)
	}
	want := "/usr/local/bin/python3.11 /usr/local/bin/gunicorn -w 4 -b 0.0.0.0:5000 --timeout 30 --access-logfile - --error-logfile - app.wsgi:app"
	if first.Command != want {
		t.Errorf("command = %q, want %q", first.Command, want)
	}
	if processes[1].PID != "3382" {
		t.Errorf("second row pid = %q, want 3382", processes[1].PID)
	}
}

// macOS lands on ps aux: USER first, COMMAND last, unprivileged users shown
// with an underscore prefix. Same parser, different column names and count.
func TestParseDockerTopMacOSFixture(t *testing.T) {
	processes := parseDockerTop(mustReadFixture(t, "top-macos.txt"))

	if len(processes) != 2 {
		t.Fatalf("expected 2 processes from top-macos fixture, got %d", len(processes))
	}
	if processes[0].User != "root" || processes[0].PID != "1" {
		t.Errorf("first row = %+v, want user root pid 1", processes[0])
	}
	if processes[1].User != "_www" {
		t.Errorf("macOS renders unprivileged users with an underscore; got %q", processes[1].User)
	}
	if !strings.HasPrefix(processes[0].Command, "caddy run") {
		t.Errorf("command = %q, want it to start with caddy run", processes[0].Command)
	}
}

func TestParseDockerTopDefensive(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		got := parseDockerTop("")
		if len(got) != 0 {
			t.Errorf("expected no processes, got %d", len(got))
		}
	})
	t.Run("header only", func(t *testing.T) {
		got := parseDockerTop("UID PID PPID C STIME TTY TIME CMD\n")
		if len(got) != 0 {
			t.Errorf("expected no processes from header alone, got %d", len(got))
		}
	})
	t.Run("no pid column", func(t *testing.T) {
		got := parseDockerTop("FOO BAR CMD\nx 1 y\n")
		if len(got) != 0 {
			t.Errorf("expected nothing parsed without a PID column, got %d", len(got))
		}
	})
	t.Run("short rows skipped", func(t *testing.T) {
		input := "UID PID CMD\n1 /bin/sh\n"
		got := parseDockerTop(input)
		// Row has fewer fields than the header: no reliable command tail.
		if len(got) != 0 {
			t.Errorf("expected short row to be skipped, got %+v", got)
		}
	})
	t.Run("blank lines ignored", func(t *testing.T) {
		input := "\nUID PID CMD\nroot 5 nginx\n\n"
		got := parseDockerTop(input)
		if len(got) != 1 || got[0].PID != "5" || got[0].User != "root" || got[0].Command != "nginx" {
			t.Errorf("unexpected parse of blank-padded output: %+v", got)
		}
	})
}

// inspect-linux.json is derived from a live `docker inspect caddy`, sanitized,
// with dual IPv4/IPv6 port bindings, an exposed-but-unpublished port, mixed
// rw/ro mounts, a health block — and a planted secret in Config.Env.
func TestParseDockerInspectLinuxFixture(t *testing.T) {
	started := time.Date(2026, 8, 21, 16, 34, 28, 132382975, time.UTC)
	now := started.Add(4 * 24 * time.Hour)

	res, err := parseDockerInspect(mustReadFixture(t, "inspect-linux.json"), now)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	if res.Name != "caddy" {
		t.Errorf("name = %q, want caddy (leading slash stripped)", res.Name)
	}
	if res.Image != "caddy:2-alpine" {
		t.Errorf("image = %q, want caddy:2-alpine", res.Image)
	}
	if res.Status != "running" || res.Uptime != "up 4d" {
		t.Errorf("state = %q uptime = %q, want running up 4d", res.Status, res.Uptime)
	}
	if res.RestartPolicy != "unless-stopped" || res.RestartCount != 0 {
		t.Errorf("restart = %s · %d, want unless-stopped · 0", res.RestartPolicy, res.RestartCount)
	}
	if res.Health != "healthy" {
		t.Errorf("health = %q, want healthy", res.Health)
	}

	// Sorted keys: 2019/tcp is exposed but unpublished (null bindings), the
	// rest carry one IPv4 and one IPv6 binding each.
	type portExpect struct {
		host      string // empty means unpublished
		container string
	}
	expects := []portExpect{
		{host: "", container: "2019/tcp"},
		{host: "0.0.0.0:443", container: "443/tcp"},
		{host: "[::]:443", container: "443/tcp"},
		{host: "0.0.0.0:443", container: "443/udp"},
		{host: "[::]:443", container: "443/udp"},
		{host: "0.0.0.0:80", container: "80/tcp"},
		{host: "[::]:80", container: "80/tcp"},
	}
	if len(res.Ports) != len(expects) {
		t.Fatalf("got %d port entries, want %d: %+v", len(res.Ports), len(expects), res.Ports)
	}
	for i, e := range expects {
		if res.Ports[i].Host != e.host || res.Ports[i].Container != e.container {
			t.Errorf("port[%d] = %+v, want %+v", i, res.Ports[i], e)
		}
	}

	if len(res.Mounts) != 4 {
		t.Fatalf("got %d mounts, want 4", len(res.Mounts))
	}
	roFile := res.Mounts[2]
	if roFile.Source != "/srv/caddy/Caddyfile" || roFile.Mode != "ro" {
		t.Errorf("Caddyfile mount = %+v, want source /srv/caddy/Caddyfile mode ro (RW=false with empty Mode)", roFile)
	}
	if res.Mounts[0].Mode != "rw" {
		t.Errorf("config mount mode = %q, want rw derived from RW=true when Mode is empty", res.Mounts[0].Mode)
	}

	if len(res.Networks) != 1 || res.Networks[0].Name != "proxy" || res.Networks[0].IP != "172.20.0.2" {
		t.Errorf("networks = %+v, want proxy at 172.20.0.2", res.Networks)
	}
}

// Neither command may emit environment variable values. The decode target has
// no field for them, and this is what proves it against a document carrying a
// planted password.
func TestInspectNeverCarriesEnvValues(t *testing.T) {
	res, err := parseDockerInspect(mustReadFixture(t, "inspect-linux.json"), time.Now())
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	out, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	encoded := string(out)
	for _, forbidden := range []string{"hunter2-planted-fake-secret", "POSTGRES_PASSWORD", `"env"`} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("inspect result leaks %s: %s", forbidden, encoded)
		}
	}
}

func TestParseDockerInspectVariants(t *testing.T) {
	stopped := `[
	  {"Name":"/backup","Image":"sha256:abc123def4567890","RestartCount":2,
	   "Config":{"Image":""},
	   "State":{"Status":"exited","Running":false,"StartedAt":"2026-08-01T00:00:00Z","FinishedAt":"2026-08-02T00:00:00Z"},
	   "HostConfig":{"RestartPolicy":{"Name":"on-failure"}},
	   "Mounts":[],"NetworkSettings":{"Ports":{},"Networks":{}}}]`

	t.Run("stopped container", func(t *testing.T) {
		res, err := parseDockerInspect(stopped, time.Now())
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if res.Status != "exited" {
			t.Errorf("status = %q, want exited", res.Status)
		}
		if res.Uptime != "" {
			t.Errorf("a stopped container carries no uptime, got %q", res.Uptime)
		}
		if res.Health != "" {
			t.Errorf("no health block means no health field, got %q", res.Health)
		}
		if res.RestartPolicy != "on-failure" || res.RestartCount != 2 {
			t.Errorf("restart = %s · %d, want on-failure · 2", res.RestartPolicy, res.RestartCount)
		}
		// Config.Image empty falls back to the resolved image ID.
		if res.Image != "sha256:abc123def4567890" {
			t.Errorf("image fallback = %q, want the image ID", res.Image)
		}
	})

	t.Run("unparseable output", func(t *testing.T) {
		if _, err := parseDockerInspect("Error: No such object: ghost", time.Now()); err == nil {
			t.Error("expected an error for non-JSON output")
		}
	})

	t.Run("empty array", func(t *testing.T) {
		if _, err := parseDockerInspect("[]", time.Now()); err == nil {
			t.Error("expected an error for an empty inspect array")
		}
	})

	t.Run("uptime not claimed before start", func(t *testing.T) {
		doc := `[{"Name":"/x","Config":{"Image":"img"},"State":{"Status":"running","Running":true,"StartedAt":"2026-08-20T00:00:00Z"}}]`
		res, err := parseDockerInspect(doc, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if res.Uptime != "" {
			t.Errorf("clock behind StartedAt must yield no uptime, got %q", res.Uptime)
		}
	})
}

// Top and Inspect validate the name before shelling out, so these run without
// a docker daemon and still prove the injection guard sits in front of exec.
func TestTopAndInspectRejectInvalidNames(t *testing.T) {
	for _, name := range []string{"", "nginx;rm -rf /", "../etc/passwd",
		"--tls", "-ldebug", "-Htcp://192.0.2.1:2375"} {
		if _, err := Top(name); err == nil {
			t.Errorf("Top(%q) passed validation", name)
		}
		if _, err := Inspect(name); err == nil {
			t.Errorf("Inspect(%q) passed validation", name)
		}
	}
}

func TestShortUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "0m"},
		{12 * time.Minute, "12m"},
		{6 * time.Hour, "6h"},
		{4 * 24 * time.Hour, "4d"},
		{400 * 24 * time.Hour, "400d"},
	}
	for _, tt := range tests {
		if got := shortUptime(tt.d); got != tt.want {
			t.Errorf("shortUptime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
