// notify/discord.go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

type Notifier struct {
	webhook string
	client  *http.Client
}

func New(url string) *Notifier {
	return &Notifier{
		webhook: url,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (n *Notifier) Send(ctx context.Context, status, message string) {
	if n.webhook == "" {
		return
	}
	var payload interface{}
	if strings.Contains(n.webhook, "hooks.slack.com") {
		text := "*" + status + "*"
		if message != "" {
			text += "\n" + message
		}
		payload = map[string]string{"text": text}
	} else {
		payload = map[string]interface{}{
			"embeds": []map[string]interface{}{
				{
					"title":       status,
					"description": message,
					"color":       3066993,
				},
			},
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[NOTIFY] Failed to marshal payload: %v", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhook, bytes.NewBuffer(data))
	if err != nil {
		log.Printf("[NOTIFY] Failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		log.Printf("[NOTIFY] Failed to send webhook notification: %v", err)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[NOTIFY] Webhook returned %s", resp.Status)
		return
	}
	log.Printf("[NOTIFY] Sent notification: %s", status)
}
