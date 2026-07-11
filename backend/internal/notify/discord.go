package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Discord struct {
	webhookURL string
	http       *http.Client
}

// mini http klijent za slanje poruke na discord o order notifikaciji
func NewDiscord(webhookURL string) *Discord {
	if webhookURL == "" {
		return nil
	}
	return &Discord{
		webhookURL: webhookURL,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// samo obican discord post za porukom o orderu
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
