package docker

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	inspectA = "7d4a91f0aa11c2b1e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7 sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85"
	inspectB = "05fcf603364d0011223344556677889900aabbccddeeff001122334455667788 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"
)

// docker inspect over several containers, one of which was removed after
// docker ps listed it: it answers for the others and puts the error in the
// same stream, since DockerCmd returns combined output.
func TestImageIDsKeepsTheContainersDockerAnsweredFor(t *testing.T) {
	raw := inspectA + "\nError: No such container: 91be0322bb22\n" + inspectB + "\n"
	asked := []string{"7d4a91f0aa11", "91be0322bb22", "05fcf603364d"}

	got, err := imageIDsFrom(asked, raw, errors.New("exit status 1"))
	want := map[string]string{
		"7d4a91f0aa11": "sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85",
		// Podman's docker CLI prints the ID without its algorithm.
		"05fcf603364d": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if err == nil || !strings.Contains(err.Error(), "2 of 3") || !strings.Contains(err.Error(), "91be0322bb22") || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("the error does not say which container was left out and why: %v", err)
	}
}

// Exiting 0 is not a clean read. An answer that leaves a container out, or
// that parses to nothing, is reported as what it is.
func TestImageIDsThatExitCleanlyCanStillBeIncomplete(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":       "",
		"unparseable": "template: :1: unexpected EOF",
		"partial":     inspectA,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := imageIDsFrom([]string{"7d4a91f0aa11", "05fcf603364d"}, raw, nil)
			if err == nil {
				t.Fatal("an incomplete answer was taken as complete")
			}
			if !strings.Contains(err.Error(), "05fcf603364d") {
				t.Errorf("the error does not name the container left out: %v", err)
			}
			if raw != "" && raw != inspectA && !strings.Contains(err.Error(), raw) {
				t.Errorf("the error drops what docker said: %v", err)
			}
		})
	}
}

func TestACompleteAnswerIsNotAnError(t *testing.T) {
	got, err := imageIDsFrom([]string{"7d4a91f0aa11", "05fcf603364d"}, inspectA+"\n"+inspectB, nil)
	if err != nil || len(got) != 2 {
		t.Errorf("got %v, %v", got, err)
	}
}

// The IDs report looks image IDs up by are the ones List returns, so the keys
// here have to be the same twelve characters docker ps prints, whatever
// length the inspect answer gives.
func TestImageIDsAreKeyedByTheIDListReturns(t *testing.T) {
	listed := parseDockerPS("7d4a91f0aa11\tnginx\tnginx:latest\tUp 2 hours\trunning\t\n" +
		"05fcf603364d\tdb\tpostgres:16\tUp 2 hours\trunning\t")
	ids := make([]string, 0, len(listed))
	for _, c := range listed {
		ids = append(ids, c.ID)
	}
	got, err := imageIDsFrom(ids, inspectA+"\n"+inspectB, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listed {
		if got[c.ID] == "" {
			t.Errorf("no image ID under %s, the ID List gave %s; keys are %v", c.ID, c.Name, got)
		}
	}
}

func TestImageIDsForNoContainersAsksDockerNothing(t *testing.T) {
	got, err := ImageIDs(nil)
	if err != nil || len(got) != 0 {
		t.Errorf("ImageIDs(nil) = %v, %v", got, err)
	}
}
