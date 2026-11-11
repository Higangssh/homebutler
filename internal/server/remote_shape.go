package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Higangssh/homebutler/internal/config"
)

// Two dashboard routes answer for this machine in one shape and forward a
// remote's CLI output, which has another. /api/processes writes the list on
// its own and `processes --json` wraps it with a total; /api/ports writes
// {"ports": [...]} and `ports --json` prints the bare list. A caller naming a
// server got whichever shape that command happened to print, and the
// dashboard's cards, written against one, showed nothing for the other.
//
// The route keeps its shape and the remote answer is fitted to it. Neither
// CLI's output changes, and entries pass through as raw JSON, so a field a
// newer remote sends is not dropped on the way.

// forwardRemoteAs forwards like forwardRemote, then reshapes what came back.
func (s *Server) forwardRemoteAs(w http.ResponseWriter, srv *config.ServerConfig, reshape func([]byte) (any, error), args ...string) {
	out, err := s.remoteRunner(srv, args...)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	body, err := reshape(out)
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid response from remote server")
		return
	}
	writeJSON(w, body)
}

var errRemoteShape = errors.New("remote answer has neither shape")

func isJSONArray(out []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(out), []byte("["))
}

// remoteProcessList turns `processes --json` into the list /api/processes
// answers with locally. A remote older than 0.12.0 already prints the list.
func remoteProcessList(out []byte) (any, error) {
	if isJSONArray(out) {
		var list []json.RawMessage
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	procs, ok := doc["processes"]
	if !ok {
		return nil, errRemoteShape
	}
	var list []json.RawMessage
	if err := json.Unmarshal(procs, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// remotePortsResult turns `ports --json` into the object /api/ports answers
// with locally. missing_process is left out: the remote's list does not
// carry it, and the local route leaves it out whenever it is false.
func remotePortsResult(out []byte) (any, error) {
	if isJSONArray(out) {
		var list []json.RawMessage
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, err
		}
		return map[string]any{"ports": list}, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	ports, ok := doc["ports"]
	if !ok {
		return nil, errRemoteShape
	}
	if string(bytes.TrimSpace(ports)) == "null" {
		doc["ports"] = json.RawMessage("[]")
	}
	return doc, nil
}
