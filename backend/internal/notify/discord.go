// Package notify posts business notifications (confirmed orders) to the shop's
// Discord channel via the webhook URL the operator injects as
// DISCORD_WEBHOOK_URL — the same webhook the shop's Alertmanager alerts use.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Discord posts messages to a single Discord webhook.
type Discord struct {
	webhookURL string
	http       *http.Client
}

// NewDiscord returns nil when webhookURL is empty (notifications disabled), so
// callers can keep a nil-checked field instead of a feature flag.
func NewDiscord(webhookURL string) *Discord {
	if webhookURL == "" {
		return nil
	}
	return &Discord{
		webhookURL: webhookURL,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Send posts a plain content message to the webhook.
func (d *Discord) Send(ctx context.Context, content string) error {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned %s", resp.Status)
	}
	return nil
}
