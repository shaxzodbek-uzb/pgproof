package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
