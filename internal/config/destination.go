package config

import "fmt"

// ErrCredentialWouldMove is returned when a patch points a stored credential at
// an address it was not given for.
//
// The reason it is worth refusing rather than warning: a saved password is sent
// to whatever answers at the address in the file, and changing that address is
// a one-field edit. Anyone who can reach the write endpoints — or who has taken
// the token from someone who can — could point a server at a machine of theirs
// and collect the password on the next refresh. Proxmox is the same shape: the
// token travels in a header to whatever host the endpoint names.
//
// Sending the credential again with the change is the whole fix. Somebody who
// knows the password can move the server; somebody who only has the config
// cannot take it with them.
var ErrCredentialWouldMove = fmt.Errorf("that change would send a saved credential to a new address")

// credentialMoveError says which item and what to do, because the refusal is
// otherwise indistinguishable from a bug in the form.
func credentialMoveError(kind, name, credential string) error {
	return fmt.Errorf("%w: %s has a saved %s, and this change points it at a different address. Send the %s again with the change, or clear it first",
		ErrCredentialWouldMove, name, credential, credential)
}

// checkDestinations refuses patches that move a stored credential.
//
// It runs against the config as it is on disk rather than against the patch
// alone, because whether a change is a move depends on where the item points
// now. It lives here rather than in the dashboard so that every caller of Save
// is held to it — the form is not the only way in, and the next one will not
// remember to ask.
func checkDestinations(current *Config, patch Patch) error {
	for _, p := range patch.Servers {
		if p.Remove {
			continue
		}
		server := current.FindServer(p.Name)
		if server == nil {
			continue // A server being added carries whatever credential it is given.
		}
		if !serverMoves(server, p) {
			continue
		}

		// The password is the credential at risk. A key is not: the private
		// half never leaves this machine, and homebutler cannot re-ask for a
		// key path from a browser anyway — #154b2 records that as a risk taken
		// deliberately rather than pretending this rule covers it.
		//
		// A password left in the file counts even when the server currently
		// signs in with a key: it is still there to be sent the moment the
		// mode is switched back.
		if server.Password == "" {
			continue
		}
		if p.Password != nil {
			continue // Sent again, or explicitly cleared.
		}
		return credentialMoveError("server", p.Name, "password")
	}

	for _, p := range patch.Proxmox {
		if p.Remove {
			continue
		}
		endpoint := findProxmox(current, p.Name)
		if endpoint == nil {
			continue
		}
		if !proxmoxMoves(endpoint, p) {
			continue
		}

		// Both credentials travel in a header to whatever host is named, so
		// each is checked on its own: an endpoint can have one, the other, or
		// both.
		for _, c := range []struct {
			stored  string
			file    string
			patched *Secret
			name    string
		}{
			{endpoint.Token, endpoint.TokenFile, p.Token, "token"},
			{endpoint.ActionToken, endpoint.ActionTokenFile, p.ActionToken, "action token"},
		} {
			if c.file != "" {
				// The credential is in a file this patch cannot name, so there
				// is no way to send it again along with the change.
				return fmt.Errorf("%w: %s reads its %s from a file, so the address cannot be changed from here. Edit the config file instead",
					ErrCredentialWouldMove, p.Name, c.name)
			}
			if c.stored == "" || c.patched != nil {
				continue
			}
			return credentialMoveError("proxmox endpoint", p.Name, c.name)
		}
	}

	return nil
}

// serverMoves reports whether the patch changes where the server is, which is
// the address, the port, and the account being signed into.
func serverMoves(server *ServerConfig, p ServerPatch) bool {
	if p.Host != nil && *p.Host != server.Host {
		return true
	}
	if p.Port != nil && *p.Port != server.SSHPort() && *p.Port != server.Port {
		return true
	}
	return p.User != nil && *p.User != server.SSHUser() && *p.User != server.User
}

func proxmoxMoves(endpoint *ProxmoxConfig, p ProxmoxPatch) bool {
	if p.Host != nil && *p.Host != endpoint.Host {
		return true
	}
	return p.Port != nil && *p.Port != endpoint.APIPort() && *p.Port != endpoint.Port
}

func findProxmox(cfg *Config, name string) *ProxmoxConfig {
	for i := range cfg.Proxmox {
		if cfg.Proxmox[i].Name == name {
			return &cfg.Proxmox[i]
		}
	}
	return nil
}
