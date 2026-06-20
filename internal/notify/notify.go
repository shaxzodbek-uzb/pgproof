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
	c  *http.Client
}

// New builds a Notifier from config.
func New(n config.Notify) *Notifier {
	return &Notifier{
		tg: n.Telegram,
		hc: n.Healthchecks,
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
}

// Failure reports a failed run.
func (n *Notifier) Failure(ctx context.Context, summary string) {
	if n.tg.Enabled && n.tg.OnFailure {
		n.telegram(ctx, "❌ pgproof FAILED\n"+summary)
	}
	if n.hc.Enabled && n.hc.PingURL != "" {
		n.ping(ctx, n.hc.PingURL+"/fail", summary)
	}
}
