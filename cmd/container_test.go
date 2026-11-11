package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/spf13/cobra"
)

// Inside a container, --all gave the local: true server the container's own
// readings under that server's name. It is refused there with the sentence
// the dashboard gives, and the SSH server is still asked.
func TestFanOutRefusesTheLocalServerInsideAContainer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")
	f := &fakeServers{local: statusJSON, remoteJSON: statusJSON}
	f.install(t)
	localRuns := 0
	localRun = func(fanOut, *config.Config, *cobra.Command) ([]byte, error) {
		localRuns++
		return []byte(statusJSON), nil
	}

	var buf bytes.Buffer
	err := fanOutAll(&buf, twoServers(), []string{"status"}, true)
	if err == nil {
		t.Fatal("expected the run to fail on the local server")
	}
	if localRuns != 0 {
		t.Fatal("the container was read as the local server")
	}

	var results []serverResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("%v: %s", err, buf.String())
	}
	if results[0].Server != "here" || results[0].Error != system.ContainerCannotSeeHost || results[0].Data != nil {
		t.Errorf("local entry: %+v", results[0])
	}
	if results[1].Server != "pi" || results[1].Error != "" || results[1].Data == nil {
		t.Errorf("the SSH server was not asked: %+v", results[1])
	}
}

// docker list reaches whatever daemon the socket leads to, which with the
// host's socket mounted is the host's, so it is not refused.
func TestFanOutStillListsDockerLocallyInsideAContainer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")
	f := &fakeServers{local: `[]`, remoteJSON: `[]`}
	f.install(t)

	var buf bytes.Buffer
	if err := fanOutAll(&buf, twoServers(), []string{"docker", "list"}, true); err != nil {
		t.Fatalf("%v: %s", err, buf.String())
	}
}

// --server naming a local: true server asks about that machine, as the
// dashboard's /api/servers/{name}/status does.
func TestServerFlagRefusesALocalServerInsideAContainer(t *testing.T) {
	t.Setenv("HOMEBUTLER_CONTAINER", "1")
	oldCfg, oldAll, oldServer := cfg, allServers, serverName
	t.Cleanup(func() { cfg, allServers, serverName = oldCfg, oldAll, oldServer })
	cfg, allServers, serverName = twoServers(), false, "here"

	for _, args := range [][]string{{"status"}, {"alerts"}, {"ports"}, {"processes"}, {"report"}, {"doctor"}, {"network", "scan"}} {
		c, _, err := rootCmd.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		handled, err := maybeRouteRemote(c)
		if !handled || !errors.Is(err, errContainerCannotSeeHost) {
			t.Errorf("%v --server here: handled=%v err=%v", args, handled, err)
		}
	}

	c, _, _ := rootCmd.Find([]string{"docker", "list"})
	if handled, err := maybeRouteRemote(c); handled || err != nil {
		t.Errorf("docker list --server here: handled=%v err=%v", handled, err)
	}
}
