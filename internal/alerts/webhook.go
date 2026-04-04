package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookPayload is the JSON body sent to webhook endpoints.
type WebhookPayload struct {
	Rule         string `json:"rule"`
	Status       string `json:"status"`
	Details      string `json:"details"`
	ActionTaken  string `json:"action_taken"`
	ActionResult string `json:"action_result"`
	Timestamp    string `json:"timestamp"`
}

// Deprecated: SendWebhook is kept for backward compatibility.
// Use NotifyAll with NotifyConfig instead.
// SendWebhook posts a JSON payload to the configured webhook URL.
// If the URL is empty, it silently returns nil (not an error).
func SendWebhook(url string, payload WebhookPayload) error {
	if url == "" {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
