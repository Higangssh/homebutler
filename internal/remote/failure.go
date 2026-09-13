package remote

import (
	"errors"
	"fmt"
)

// FailureClass is what went wrong, in terms safe to hand to a browser.
//
// The messages this package returns are written for a terminal: they name the
// host and port, the config file, ~/.ssh/known_hosts, and the remote command's
// own output. That is right for someone who can act on it and wrong for a
// dashboard, which is why #151 drew the same line for Proxmox — a bare error
// leaves every consumer to invent its own meaning.
type FailureClass string

const (
	// The host did not answer: down, wrong address, or a firewall in the way.
	ClassUnreachable FailureClass = "unreachable"
	// The host answered and presented a key that is not the one we trust, or
	// one that could not be registered. Never collapse this into unreachable:
	// it is the only class that can mean someone is in the middle.
	ClassHostKey FailureClass = "host_key"
	// The connection was made and the credentials were refused, or there were
	// none to offer.
	ClassAuthentication FailureClass = "authentication"
	// Connected and authenticated, and the command on the far side failed —
	// homebutler missing from PATH is the common one.
	ClassRemote FailureClass = "remote"
)

// Error carries a class alongside the message the CLI has always printed.
// Error() delegates, so terminal output is byte-for-byte what it was.
type Error struct {
	Class FailureClass
	Err   error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func classified(class FailureClass, format string, args ...any) error {
	return &Error{Class: class, Err: fmt.Errorf(format, args...)}
}

// Classify reports the class of err, or ClassUnreachable when it carries none.
// Unreachable is the safe default: it says the least about a machine we could
// not reach, and it never claims a host key problem that did not happen.
func Classify(err error) FailureClass {
	if err == nil {
		return ""
	}
	var re *Error
	if errors.As(err, &re) {
		return re.Class
	}
	return ClassUnreachable
}

// Describe is the one sentence a browser may be shown. It says what to do
// next without naming an address, a path, or anything the remote printed.
func Describe(class FailureClass) string {
	switch class {
	case ClassHostKey:
		return "The host key does not match the one homebutler trusts. Check it from a terminal before connecting again."
	case ClassAuthentication:
		return "The server refused the configured credentials."
	case ClassRemote:
		return "The server answered but homebutler could not run there."
	default:
		return "The server did not answer."
	}
}
