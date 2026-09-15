package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

const testConfigFile = `servers:
  - name: local
    host: 127.0.0.1
    local: true
alerts:
  cpu: 90
  memory: 85
  disk: 90
`

// writableServer is a serve that can actually write: a real file on disk and a
// token, because the write routes do not exist without one.
func writableServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)
	srv.SetToken("test-token")
	return srv, path
}

func do(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// currentRevision is what a page holds: the token the dashboard was handed,
// not anything computed from the file.
func currentRevision(t *testing.T, srv *Server) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(do(t, srv, "GET", "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	rev, _ := body["revision"].(string)
	if rev == "" {
		t.Fatal("the config response carried no revision")
	}
	return rev
}

// TestSaveAndReadConcurrently is the test the race needed. A save replaces the
// config the process holds while requests are reading it, and nothing but
// concurrent traffic makes that visible: the rest of the suite sends one
// request at a time and stayed green through the bug.
func TestSaveAndReadConcurrently(t *testing.T) {
	srv, _ := writableServer(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			rev := currentRevision(t, srv)
			do(t, srv, "PUT", "/api/config/alerts", fmt.Sprintf(`{"revision":%q,"cpu":%d}`, rev, 70+i%20))
		}(i)
		go func() {
			defer wg.Done()
			for _, path := range []string{"/api/config", "/api/overview", "/api/servers", "/api/wake"} {
				do(t, srv, "GET", path, "")
			}
		}()
	}
	wg.Wait()

	// Whichever save won, the process and the file agree afterwards.
	var body map[string]any
	if err := json.Unmarshal(do(t, srv, "GET", "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	served := body["alerts"].(map[string]any)["cpu"].(float64)
	onDisk, err := config.Load(srv.config().Path)
	if err != nil {
		t.Fatal(err)
	}
	if served != onDisk.Alerts.CPU {
		t.Fatalf("serve reports cpu %v, the file says %v", served, onDisk.Alerts.CPU)
	}
}

func TestSaveRejectsARevisionFromAnotherProcess(t *testing.T) {
	srv, path := writableServer(t)
	stale := currentRevision(t, srv)

	// A restarted serve reading the same unchanged file. The page is looking
	// at current content, but its token was signed by a key that is gone.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(cfg, "127.0.0.1", 8080)
	restarted.SetToken("test-token")

	if got := currentRevision(t, restarted); got == stale {
		t.Fatal("two processes issued the same revision token, so it is not keyed per process")
	}
	if w := do(t, restarted, "PUT", "/api/config/alerts", fmt.Sprintf(`{"revision":%q,"cpu":75}`, stale)); w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a token from another process, got %d: %s", w.Code, w.Body)
	}
}

// The revision is a handle on the file's contents, so a dashboard with no way
// to send it back is not given one.
func TestReadOnlyDashboardGetsNoRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["editable"] != false {
		t.Fatalf("a dashboard without a token reported editable %v", body["editable"])
	}
	if _, ok := body["revision"]; ok {
		t.Fatalf("a read-only dashboard was handed a revision: %v", body["revision"])
	}
}

func TestSaveRefusesAnOversizedBody(t *testing.T) {
	srv, _ := writableServer(t)
	rev := currentRevision(t, srv)
	body := fmt.Sprintf(`{"revision":%q,"cpu":75,"padding":%q}`, rev, strings.Repeat("x", maxSaveRequest+1))

	if w := do(t, srv, "PUT", "/api/config/alerts", body); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body)
	}
}

const notifyConfigFile = `servers:
  - name: local
    host: 127.0.0.1
    local: true
notify:
  ntfy:
    url: https://ntfy.sh
    topic: keep-me            # not guessable, which is the point
    token: tk_keepme
`

func notifyServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(notifyConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)
	srv.SetToken("test-token")
	return srv, path
}

func channel(t *testing.T, srv *Server, name string) channelSettings {
	t.Helper()
	var body struct {
		Notify []channelSettings `json:"notify"`
	}
	if err := json.Unmarshal(do(t, srv, "GET", "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, c := range body.Notify {
		if c.Channel == name {
			return c
		}
	}
	t.Fatalf("no %s channel in the config response", name)
	return channelSettings{}
}

func field(t *testing.T, c channelSettings, name string) channelField {
	t.Helper()
	for _, f := range c.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("channel %s has no field %s", c.Channel, name)
	return channelField{}
}

// The form sends what was typed. An untouched credential box is not sent at
// all, and a save that changes the server address beside it must not take the
// silence for a deletion — that loses a working setup because somebody fixed
// a URL.
func TestSavingAroundACredentialKeepsIt(t *testing.T) {
	srv, path := notifyServer(t)

	body := fmt.Sprintf(`{"revision":%q,"channels":{"ntfy":{"values":{"url":"https://ntfy.example.com"}}}}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/notify", body); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}

	ntfy := channel(t, srv, "ntfy")
	if got := field(t, ntfy, "url").Value; got != "https://ntfy.example.com" {
		t.Fatalf("the url was not saved: %q", got)
	}
	for _, name := range []string{"topic", "token"} {
		if set := field(t, ntfy, name).Set; set == nil || !*set {
			t.Fatalf("%s stopped being set after a save that never mentioned it", name)
		}
	}

	// And the credential itself is still in the file, not merely reported as
	// present.
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "tk_keepme") {
		t.Fatalf("the token is gone from the file:\n%s", saved)
	}
}

// Clearing is its own action, so an empty box cannot delete a working setup by
// accident.
func TestClearingACredentialIsDeliberate(t *testing.T) {
	srv, _ := notifyServer(t)

	body := fmt.Sprintf(`{"revision":%q,"channels":{"ntfy":{"secrets":{"token":{"clear":true}}}}}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/notify", body); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}

	ntfy := channel(t, srv, "ntfy")
	if set := field(t, ntfy, "token").Set; set == nil || *set {
		t.Fatal("the token was asked to be cleared and still reports as set")
	}
	if set := field(t, ntfy, "topic").Set; set == nil || !*set {
		t.Fatal("clearing the token cleared the topic as well")
	}
}

// A credential never reaches the browser, in either direction of the shape:
// a secret field carries only whether one is set, and a plain field carries
// only a value.
func TestNoCredentialIsServedToTheDashboard(t *testing.T) {
	srv, _ := notifyServer(t)
	raw := do(t, srv, "GET", "/api/config", "").Body.String()

	if strings.Contains(raw, "tk_keepme") || strings.Contains(raw, "keep-me") {
		t.Fatalf("a credential was served to the dashboard:\n%s", raw)
	}
	for _, f := range channel(t, srv, "ntfy").Fields {
		if f.Secret && f.Value != "" {
			t.Fatalf("secret field %s carried a value", f.Name)
		}
		if !f.Secret && f.Set != nil {
			t.Fatalf("plain field %s carried a set flag", f.Name)
		}
	}
}

// The fields arrive in the order the provider declares them, which is the
// order the form is laid out in. A map would have lost it.
func TestChannelFieldsKeepTheirOrder(t *testing.T) {
	srv, _ := notifyServer(t)
	var names []string
	for _, f := range channel(t, srv, "ntfy").Fields {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "url,topic,token" {
		t.Fatalf("ntfy fields came back as %v", names)
	}
	if !field(t, channel(t, srv, "ntfy"), "token").Optional {
		t.Fatal("the ntfy token is optional and did not say so")
	}
}

func TestNotifyTestNeedsAChannel(t *testing.T) {
	srv, _ := writableServer(t)
	if w := do(t, srv, "POST", "/api/notify/test", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with nothing configured, got %d: %s", w.Code, w.Body)
	}
}

// Sending a message is a write, so the route does not exist without a token.
func TestNotifyTestIsAbsentWithoutAToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(notifyConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/notify/test", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected the route not to exist, got %d", w.Code)
	}
}

const wakeConfigFile = `servers:
  - name: local
    host: 127.0.0.1
    local: true
wake:
  - name: gaming-pc
    mac: AA:BB:CC:DD:EE:FF
`

func wakeServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(wakeConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)
	srv.SetToken("test-token")
	return srv, path
}

func wakeTargets(t *testing.T, srv *Server) []map[string]string {
	t.Helper()
	var body struct {
		Wake []map[string]string `json:"wake"`
	}
	if err := json.Unmarshal(do(t, srv, "GET", "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Wake
}

func TestSaveAddsAWakeTarget(t *testing.T) {
	srv, path := wakeServer(t)

	body := fmt.Sprintf(`{"revision":%q,"targets":[{"name":"nas","mac":"11:22:33:44:55:66","broadcast":"192.168.1.255"}]}`, currentRevision(t, srv))
	w := do(t, srv, "PUT", "/api/config/wake", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	// A wake target works as soon as it is written: serve reads the file back
	// and the CLI reads it every run, so nothing is waiting on a restart.
	if strings.Contains(w.Body.String(), "restart_needed") {
		t.Fatalf("a wake save asked for a restart: %s", w.Body)
	}

	targets := wakeTargets(t, srv)
	if len(targets) != 2 {
		t.Fatalf("expected two targets, got %v", targets)
	}
	if targets[1]["name"] != "nas" || targets[1]["broadcast"] != "192.168.1.255" {
		t.Fatalf("the new target came back wrong: %v", targets[1])
	}

	// The file says `ip`, whatever the JSON calls it.
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "ip: 192.168.1.255") {
		t.Fatalf("the broadcast address is not under the key the file uses:\n%s", saved)
	}
}

// Nothing answers a magic packet, so a typed address is only ever discovered
// as a machine that did not turn on. It is refused at the save instead.
func TestSaveRefusesAWakeTargetWithABadMAC(t *testing.T) {
	srv, path := wakeServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"revision":%q,"targets":[{"name":"nas","mac":"11:22:33:44:55"}]}`, currentRevision(t, srv))
	w := do(t, srv, "PUT", "/api/config/wake", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "AA:BB:CC:DD:EE:FF") {
		t.Fatalf("the refusal does not say what a MAC looks like: %s", w.Body)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the file was written anyway")
	}
}

func TestSaveRemovesAWakeTarget(t *testing.T) {
	srv, _ := wakeServer(t)

	body := fmt.Sprintf(`{"revision":%q,"targets":[{"name":"gaming-pc","remove":true}]}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/wake", body); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	if targets := wakeTargets(t, srv); len(targets) != 0 {
		t.Fatalf("the target is still there: %v", targets)
	}
}

const serversConfigFile = `servers:
  - name: nas
    host: 192.168.0.9
    user: admin
    auth: password
    password: hunter2
  - name: pi
    host: 192.168.0.4
    user: pi
proxmox:
  - name: pve
    host: 192.168.0.50
    token_id: root@pam!homebutler
    token: tok_read
`

func serversServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(serversConfigFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, "127.0.0.1", 8080)
	srv.SetToken("test-token")
	return srv, path
}

// The rule lives in the writer, so the endpoint's job is to carry the refusal
// back in words the person reading the form can act on.
func TestMovingAServerFromTheDashboardNeedsThePasswordAgain(t *testing.T) {
	srv, path := serversServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"revision":%q,"servers":[{"name":"nas","host":"10.0.0.6"}]}`, currentRevision(t, srv))
	w := do(t, srv, "PUT", "/api/config/servers", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "Send the password again") {
		t.Fatalf("the refusal does not say what to do: %s", w.Body)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the file was written anyway")
	}
}

func TestMovingAServerWithThePasswordSucceeds(t *testing.T) {
	srv, _ := serversServer(t)

	body := fmt.Sprintf(`{"revision":%q,"servers":[{"name":"nas","host":"10.0.0.6","password":{"value":"a-new-one"}}]}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/servers", body); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	if got := srv.config().FindServer("nas"); got == nil || got.Host != "10.0.0.6" {
		t.Fatalf("the change did not land: %+v", got)
	}
}

// A key server's address can move, and what that means is said rather than
// refused: the new address is trusted the first time homebutler connects.
func TestMovingAKeyServerSaysTheNewAddressWillBeTrusted(t *testing.T) {
	srv, _ := serversServer(t)

	body := fmt.Sprintf(`{"revision":%q,"servers":[{"name":"pi","host":"192.168.0.44"}]}`, currentRevision(t, srv))
	w := do(t, srv, "PUT", "/api/config/servers", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}

	var response saveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Notices) != 1 || !strings.Contains(response.Notices[0], "trusted the first time") {
		t.Fatalf("the move was not explained: %v", response.Notices)
	}
}

func TestAddingAndRemovingAServerFromTheDashboard(t *testing.T) {
	srv, _ := serversServer(t)

	add := fmt.Sprintf(`{"revision":%q,"servers":[{"name":"media","host":"192.168.0.20","user":"media","auth":"key"}]}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/servers", add); w.Code != http.StatusOK {
		t.Fatalf("adding: expected 200, got %d: %s", w.Code, w.Body)
	}
	if got := srv.config().FindServer("media"); got == nil || got.Host != "192.168.0.20" {
		t.Fatalf("the server was not added: %+v", srv.config().Servers)
	}

	remove := fmt.Sprintf(`{"revision":%q,"servers":[{"name":"media","remove":true}]}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/servers", remove); w.Code != http.StatusOK {
		t.Fatalf("removing: expected 200, got %d: %s", w.Code, w.Body)
	}
	if got := srv.config().FindServer("media"); got != nil {
		t.Fatalf("the server was not removed: %+v", got)
	}
	if got := srv.config().FindServer("nas"); got == nil || got.Password != "hunter2" {
		t.Fatalf("removing one server disturbed another: %+v", got)
	}
}

// A rename keeps everything else about the server, which is the difference
// between renaming and deleting-then-adding.
func TestRenamingAServerKeepsTheRest(t *testing.T) {
	srv, _ := serversServer(t)

	body := fmt.Sprintf(`{"revision":%q,"servers":[{"name":"nas","rename":"storage"}]}`, currentRevision(t, srv))
	if w := do(t, srv, "PUT", "/api/config/servers", body); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}

	cfg := srv.config()
	if cfg.FindServer("nas") != nil {
		t.Fatal("the old name is still there")
	}
	got := cfg.FindServer("storage")
	if got == nil || got.Host != "192.168.0.9" || got.Password != "hunter2" {
		t.Fatalf("the rename lost something: %+v", got)
	}
}

func TestMovingAProxmoxEndpointFromTheDashboardNeedsItsToken(t *testing.T) {
	srv, _ := serversServer(t)

	body := fmt.Sprintf(`{"revision":%q,"endpoints":[{"name":"pve","host":"10.0.0.50"}]}`, currentRevision(t, srv))
	w := do(t, srv, "PUT", "/api/config/proxmox", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "token") {
		t.Fatalf("the refusal does not name the credential: %s", w.Body)
	}
}

// No credential is readable from the dashboard, whatever the section.
func TestServerPasswordsAreNotServed(t *testing.T) {
	srv, _ := serversServer(t)
	raw := do(t, srv, "GET", "/api/config", "").Body.String()

	if strings.Contains(raw, "hunter2") || strings.Contains(raw, "tok_read") {
		t.Fatalf("a credential was served to the dashboard:\n%s", raw)
	}
	if !strings.Contains(raw, `"password_set":true`) {
		t.Fatalf("the dashboard cannot tell that a password is set:\n%s", raw)
	}
}
