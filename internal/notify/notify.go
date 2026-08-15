// Package notify sends run outcomes to Telegram and/or a healthchecks.io-style
// dead-man's-switch URL.
package notify

import (
	"context"
	"net/http"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// Notifier fans a run outcome out to the configured channels.
type Notifier struct {
	tg config.TelegramNotify
	hc config.HealthchecksNotify
	wh config.WebhookNotify
	c  *http.Client
}

// New builds a Notifier from config.
func New(n config.Notify) *Notifier {
	return &Notifier{
		tg: n.Telegram,
		hc: n.Healthchecks,
		wh: n.Webhook,
		c:  &http.Client{Timeout: 20 * time.Second},
	}
}

// Start signals the beginning of a run (healthchecks /start).
func (n *Notifier) Start(ctx context.Context) {
	if n.hc.Enabled && n.hc.PingURL != "" {
		n.ping(ctx, n.hc.PingURL+"/start", "")
	}
}

// Success reports a successful run.
func (n *Notifier) Success(ctx context.Context, summary string) {
	if n.tg.Enabled && n.tg.OnSuccess {
		n.telegram(ctx, "✅ pgproof\n"+summary)
	}
	if n.hc.Enabled && n.hc.PingURL != "" {
		n.ping(ctx, n.hc.PingURL, summary)
	}
	if n.wh.Enabled && n.wh.OnSuccess {
		n.webhook(ctx, "success", "✅ pgproof\n"+summary, summary)
	}
}

// Failure reports a failed run.
func (n *Notifier) Failure(ctx context.Context, summary string) {
	if n.tg.Enabled && n.tg.OnFailure {
		n.telegram(ctx, "❌ pgproof FAILED\n"+summary)
	}
	if n.hc.Enabled && n.hc.PingURL != "" {
		n.ping(ctx, n.hc.PingURL+"/fail", summary)
	}
	if n.wh.Enabled && n.wh.OnFailure {
		n.webhook(ctx, "failure", "❌ pgproof FAILED\n"+summary, summary)
	}
}
