package notify

import "time"

type Event struct {
	Kind        string    `json:"kind,omitempty"`
	Source      string    `json:"source"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Details     string    `json:"details"`
	Action      string    `json:"action,omitempty"`
	Result      string    `json:"result,omitempty"`
	Time        time.Time `json:"time"`
	Channels    []Channel `json:"channels,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
}
