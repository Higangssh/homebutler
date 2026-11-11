package remote

import (
	"bytes"
	"encoding/json"
	"errors"
)

// A command forwarded to a remote homebutler answers with whatever its CLI
// prints, and a few of those print another shape than the local answer they
// stand in for. `ports --json` prints the bare list where /api/ports and the
// open_ports tool answer {"ports": [...]}; `processes --json` wraps the list
// with a total where /api/processes answers the list on its own.
//
// These fit the remote answer to the local one, for the dashboard and the MCP
// server alike. Neither CLI's output changes, and entries pass through as raw
// JSON, so a field a newer remote sends is not dropped on the way.

// ErrUnexpectedShape is a remote answer that is JSON but not either shape the
// command has printed.
var ErrUnexpectedShape = errors.New("remote answer has neither shape")

func isJSONArray(out []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(out), []byte("["))
}

// decodeList decodes a JSON list, taking null as the empty list. Anything
// else, an object included, is ErrUnexpectedShape: an object where a list
// belongs is not an empty table.
func decodeList(raw []byte) ([]json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if string(raw) == "null" {
		return []json.RawMessage{}, nil
	}
	if !isJSONArray(raw) {
		return nil, ErrUnexpectedShape
	}
	list := []json.RawMessage{}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ProcessList turns `processes --json` into the list /api/processes answers
// with locally. A remote older than 0.12.0 already prints the list.
func ProcessList(out []byte) ([]json.RawMessage, error) {
	if isJSONArray(out) {
		return decodeList(out)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	procs, ok := doc["processes"]
	if !ok {
		return nil, ErrUnexpectedShape
	}
	return decodeList(procs)
}

// PortsResult turns `ports --json` into ports.Result, the object open_ports
// and /api/ports answer with locally. Every release so far prints the bare
// list; the object is accepted too, so a remote that starts printing it does
// not break this. missing_process is left out for the list: the remote's list
// does not carry it, and the local answer leaves it out whenever it is false.
func PortsResult(out []byte) (map[string]json.RawMessage, error) {
	if isJSONArray(out) {
		list, err := decodeList(out)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(list)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{"ports": data}, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	raw, ok := doc["ports"]
	if !ok {
		return nil, ErrUnexpectedShape
	}
	list, err := decodeList(raw)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	doc["ports"] = data
	return doc, nil
}
