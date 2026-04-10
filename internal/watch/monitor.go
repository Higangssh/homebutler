package watch

import "context"

// Monitor watches a set of targets and sends incidents to the channel.
// Watch blocks until ctx is cancelled or an unrecoverable error occurs.
type Monitor interface {
	Watch(ctx context.Context, targets []Target, incidents chan<- Incident) error
}

// CommandRunner abstracts external command execution for testability.
type CommandRunner func(name string, args ...string) (string, error)
