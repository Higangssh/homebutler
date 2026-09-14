package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ntfy and Gotify are the two servers people self-host to receive exactly this
// kind of message. Neither takes homebutler's webhook payload: ntfy wants the
// message as the request body with the title and priority as headers, and
// Gotify wants {title, message, priority} with an application token.

// pushTitle is the one line a phone shows before it is unlocked, so it names
// what happened and to what, and nothing else.
func pushTitle(event Event) string {
	name := event.Name
	if name == "" {
		name = event.Source
	}
	if name == "" {
		return "homebutler"
	}
	return fmt.Sprintf("homebutler: %s %s", name, event.Status)
}

// pushBody is what is read after unlocking: the detail, and what homebutler did
// about it when it did anything.
func pushBody(event Event) string {
	lines := []string{}
	if event.Details != "" {
		lines = append(lines, event.Details)
	}
	if event.Action != "" {
		action := "Action: " + event.Action
		if event.Result != "" {
			action += " (" + event.Result + ")"
		}
		lines = append(lines, action)
	}
	if !event.Time.IsZero() {
		lines = append(lines, event.Time.Format("2006-01-02 15:04:05"))
	}
	if len(lines) == 0 {
		return event.Status
	}
	return strings.Join(lines, "\n")
}

// ntfyPriority and gotifyPriority are the two servers' own scales. 4 and 8 are
// each server's "high", the level that bypasses a quiet-hours rule on a phone.
func ntfyPriority(p Priority) string {
	if p == PriorityHigh {
		return "4"
	}
	return "3"
}

func gotifyPriority(p Priority) int {
	if p == PriorityHigh {
		return 8
	}
	return 4
}

// sendNtfy publishes to a topic. The message is the body and everything else
// is a header, which is ntfy's own shape rather than a translation of ours.
func sendNtfy(cfg *NtfyConfig, event Event) error {
	endpoint, err := url.JoinPath(cfg.URL, cfg.Topic)
	if err != nil {
		return fmt.Errorf("invalid ntfy url or topic: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(pushBody(event)))
	if err != nil {
		return fmt.Errorf("invalid ntfy request: %w", err)
	}
	req.Header.Set("Title", pushTitle(event))
	req.Header.Set("Priority", ntfyPriority(priorityFor(event)))
	// The token is a header rather than a query parameter so that an error
	// quoting the request cannot carry it (#176).
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	return doPush(req, "ntfy")
}

// sendGotify posts a message to an application. The token identifies which
// application it belongs to, so Gotify has no unauthenticated publish.
func sendGotify(cfg *GotifyConfig, event Event) error {
	endpoint, err := url.JoinPath(cfg.URL, "message")
	if err != nil {
		return fmt.Errorf("invalid gotify url: %w", err)
	}

	payload := map[string]any{
		"title":    pushTitle(event),
		"message":  pushBody(event),
		"priority": gotifyPriority(priorityFor(event)),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid gotify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", cfg.Token)

	return doPush(req, "gotify")
}

// doPush sends a prepared request and reports the outcome without the address,
// on the same terms as postJSON: the scheme and host survive and the path,
// query and any credential do not.
func doPush(req *http.Request, what string) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("request to %s failed: %w", safeAddress(urlErr.URL), urlErr.Err)
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("returned status %d", resp.StatusCode)
	}
	return nil
}
