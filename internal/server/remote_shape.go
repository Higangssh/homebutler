package server

import (
	"net/http"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

// Two dashboard routes answer for this machine in one shape and forward a
// remote's CLI output, which has another. The route keeps its shape and the
// remote answer is fitted to it by internal/remote's shape functions, the
// same ones the MCP server uses, so the dashboard and an agent cannot come to
// disagree about what a remote answer turns into.

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

func remoteProcessList(out []byte) (any, error) { return remote.ProcessList(out) }

func remotePortsResult(out []byte) (any, error) { return remote.PortsResult(out) }
