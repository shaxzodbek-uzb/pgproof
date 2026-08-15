package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// telegram posts a message to the configured chat. Errors are swallowed:
// a failed notification must never fail or mask a backup outcome.
func (n *Notifier) telegram(ctx context.Context, text string) {
	if n.tg.BotToken == "" || n.tg.ChatID == "" {
		return
	}
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", url.PathEscape(n.tg.BotToken))
	form := url.Values{}
	form.Set("chat_id", n.tg.ChatID)
	form.Set("text", truncate(text, 3900)) // Telegram message hard limit is 4096
	form.Set("disable_web_page_preview", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := n.c.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// WebhookPayload is the JSON body POSTed to a configured webhook.
//
// The "text" field is first so Slack and Discord incoming webhooks work with no
// extra configuration; anything else can read the structured fields instead.
type WebhookPayload struct {
	Text    string `json:"text"`
	Tool    string `json:"tool"`
	Status  string `json:"status"` // success | failure
	Summary string `json:"summary"`
	SentAt  string `json:"sent_at"`
}

// webhook POSTs a JSON run summary. Like every other channel here, errors are
// swallowed: a failed notification must never fail or mask a backup outcome.
func (n *Notifier) webhook(ctx context.Context, status, text, summary string) {
	if n.wh.URL == "" {
		return
	}
	body, err := json.Marshal(WebhookPayload{
		Text:    truncate(text, 3900),
		Tool:    "pgproof",
		Status:  status,
		Summary: truncate(summary, 10000),
		SentAt:  time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.wh.URL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.wh.Headers {
		req.Header.Set(k, v)
	}
	resp, err := n.c.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// ping fires a GET (with an optional body) at a healthchecks-style URL.
func (n *Notifier) ping(ctx context.Context, target, body string) {
	var reader io.Reader
	method := http.MethodGet
	if body != "" {
		reader = strings.NewReader(truncate(body, 10000))
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return
	}
	resp, err := n.c.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
