package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/remote"
	"github.com/Higangssh/homebutler/internal/watch"
)

// remoteAnswer fits what a remote homebutler printed to what the tool answers
// with when it runs here. docs/compatibility.md freezes one answer per tool,
// and naming a server is not a way to get a different one.
//
// Most commands already print the tool's answer. The ones that do not:
// `ports --json` prints the bare list, and through 1.7.0 `watch add`, `watch
// remove` and `watch check` with nothing to check printed a sentence even
// under --json. A remote on an older release is what most fleets have, so
// those sentences are read here rather than refused; they are fixed text in
// releases that are already out. watch_check has been forwarded since
// 0.21.0, so its sentence is read in both wordings: `No targets being
// watched.` from 0.21.0 on, `No containers being watched.` before that,
// back to 0.15.0.
func remoteAnswer(tool string, args map[string]any, out []byte) (any, error) {
	switch tool {
	case "docker_top":
		return remoteDockerTop(out)
	case "open_ports":
		return shaped(remote.PortsResult(out))
	case "processes":
		return remoteProcesses(out)
	case "watch_check":
		return remoteWatchCheck(out)
	case "watch_add":
		return remoteWatchAdd(out, stringArg(args, "container"), watchAddKind(args))
	case "watch_remove":
		return remoteWatchRemove(out, stringArg(args, "container"))
	}
	var result any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON from remote: %w", err)
	}
	return result, nil
}

// shaped words a shape function's error. JSON in the wrong shape is not
// invalid JSON, and saying so sends someone looking for a corrupted answer.
func shaped[T any](v T, err error) (any, error) {
	if errors.Is(err, remote.ErrUnexpectedShape) {
		return nil, fmt.Errorf("unexpected answer from remote: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid JSON from remote: %w", err)
	}
	return v, nil
}

// watchAddKind is the kind executeRemote sends, so the answer built from an
// older remote's sentence names the kind that was asked for.
func watchAddKind(args map[string]any) string {
	if kind := stringArg(args, "kind"); kind != "" {
		return kind
	}
	return watch.KindDocker
}

// remoteProcesses checks that `processes --json` is the ProcessResult the
// tool answers with locally. A remote older than 0.12.0 printed the bare list,
// which has no total to fill the answer in with, so it is refused rather than
// passed on in another shape.
func remoteProcesses(out []byte) (any, error) {
	if bytes.HasPrefix(bytes.TrimSpace(out), []byte("[")) {
		return nil, fmt.Errorf("the remote homebutler is older than 0.12.0 and answers processes without a total; upgrade it with homebutler upgrade")
	}
	doc, err := remote.Object(out)
	if err != nil {
		return shaped(doc, err)
	}
	// ProcessList takes a null list as the empty one, and that is the list the
	// answer carries: an MCP answer's lists are never null.
	list, err := remote.ProcessList(out)
	if err != nil {
		return shaped(list, err)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	doc["processes"] = data
	return doc, nil
}

// watchCheckEmpty is what `watch check` printed under --json when nothing
// was watched, in each wording a release forwarded to has used.
var watchCheckEmpty = map[string]bool{
	"No targets being watched.":    true, // 0.21.0 to 1.7.0
	"No containers being watched.": true, // 0.15.0 to 0.20.0
}

func remoteWatchCheck(out []byte) (any, error) {
	if watchCheckEmpty[strings.TrimSpace(string(out))] {
		return watch.RunResult{}, nil
	}
	return remoteObject(out)
}

func remoteWatchAdd(out []byte, container, kind string) (any, error) {
	if json.Valid(out) {
		return remoteObject(out)
	}
	first := firstLine(out)
	switch {
	case strings.HasPrefix(first, "Added "):
		return watch.WatchAddResult{Container: container, Kind: kind, Added: true}, nil
	case strings.HasSuffix(first, " is already being watched."):
		return watch.WatchAddResult{Container: container, Kind: kind, Added: false}, nil
	}
	return nil, fmt.Errorf("unexpected answer from remote: %q", first)
}

// A remote that could not remove the target exits non-zero, which never
// reaches here, so its sentence only ever says the target was removed.
func remoteWatchRemove(out []byte, container string) (any, error) {
	if json.Valid(out) {
		return remoteObject(out)
	}
	if first := firstLine(out); !strings.HasPrefix(first, "Removed ") {
		return nil, fmt.Errorf("unexpected answer from remote: %q", first)
	}
	return watch.WatchRemoveResult{Container: container, Removed: true}, nil
}

// remoteObject passes an object through and refuses anything else, so a list
// or a sentence cannot stand in for a result.
func remoteObject(out []byte) (any, error) {
	return shaped(remote.Object(out))
}

func firstLine(out []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}
