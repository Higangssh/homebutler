package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Higangssh/homebutler/internal/notify"
)

// WebhookConfig is an alias for backward compatibility.
type WebhookConfig = notify.WebhookConfig

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
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
