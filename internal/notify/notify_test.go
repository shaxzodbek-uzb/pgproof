package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// recorder captures what a webhook receiver saw.
type recorder struct {
	mu       sync.Mutex
	calls    int
	body     []byte
	headers  http.Header
	respCode int
}

func (r *recorder) handler(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.body, _ = io.ReadAll(req.Body)
	r.headers = req.Header.Clone()
	if r.respCode != 0 {
		w.WriteHeader(r.respCode)
	}
}

func (r *recorder) payload(t *testing.T) WebhookPayload {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var p WebhookPayload
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, r.body)
	}
	return p
}

func newWebhook(t *testing.T, rec *recorder, cfg config.WebhookNotify) *Notifier {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	t.Cleanup(srv.Close)
	cfg.URL = srv.URL
	return New(config.Notify{Webhook: cfg})
}

func TestWebhookPostsOnSuccess(t *testing.T) {
	rec := &recorder{}
	n := newWebhook(t, rec, config.WebhookNotify{Enabled: true, OnSuccess: true})
	n.Success(context.Background(), "app: 24.1 MiB, verified")

	if rec.calls != 1 {
		t.Fatalf("calls = %d, want 1", rec.calls)
	}
	p := rec.payload(t)
	if p.Status != "success" {
		t.Errorf("status = %q, want success", p.Status)
	}
	if p.Tool != "pgproof" {
		t.Errorf("tool = %q", p.Tool)
	}
	if !strings.Contains(p.Summary, "24.1 MiB") {
		t.Errorf("summary lost the detail: %q", p.Summary)
	}
	// Slack and Discord read "text"; it must be populated, not just the structured fields.
	if !strings.Contains(p.Text, "24.1 MiB") {
		t.Errorf("text = %q, want the summary included", p.Text)
	}
	if p.SentAt == "" {
		t.Error("sent_at is empty")
	}
}

func TestWebhookPostsOnFailure(t *testing.T) {
	rec := &recorder{}
	n := newWebhook(t, rec, config.WebhookNotify{Enabled: true, OnFailure: true})
	n.Failure(context.Background(), "app: dump failed")

	if rec.calls != 1 {
		t.Fatalf("calls = %d, want 1", rec.calls)
	}
	if got := rec.payload(t).Status; got != "failure" {
		t.Errorf("status = %q, want failure", got)
	}
}

func TestWebhookRespectsOnSuccessAndOnFailure(t *testing.T) {
	rec := &recorder{}
	n := newWebhook(t, rec, config.WebhookNotify{Enabled: true, OnFailure: true})
	n.Success(context.Background(), "fine")
	if rec.calls != 0 {
		t.Fatalf("on_success=false still posted %d time(s)", rec.calls)
	}
	n.Failure(context.Background(), "broken")
	if rec.calls != 1 {
		t.Fatalf("calls = %d, want 1", rec.calls)
	}
}

func TestDisabledWebhookNeverPosts(t *testing.T) {
	rec := &recorder{}
	n := newWebhook(t, rec, config.WebhookNotify{Enabled: false, OnSuccess: true, OnFailure: true})
	n.Success(context.Background(), "x")
	n.Failure(context.Background(), "y")
	if rec.calls != 0 {
		t.Fatalf("disabled webhook posted %d time(s)", rec.calls)
	}
}

func TestWebhookSendsConfiguredHeaders(t *testing.T) {
	rec := &recorder{}
	n := newWebhook(t, rec, config.WebhookNotify{
		Enabled:   true,
		OnSuccess: true,
		Headers:   map[string]string{"Authorization": "Bearer s3cret", "X-Env": "prod"},
	})
	n.Success(context.Background(), "ok")

	if got := rec.headers.Get("Authorization"); got != "Bearer s3cret" {
		t.Errorf("Authorization = %q", got)
	}
	if got := rec.headers.Get("X-Env"); got != "prod" {
		t.Errorf("X-Env = %q", got)
	}
	if got := rec.headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestAnEmptyURLIsNotPosted(t *testing.T) {
	// Enabled but unconfigured must be a no-op, not a request to "".
	n := New(config.Notify{Webhook: config.WebhookNotify{Enabled: true, OnSuccess: true}})
	n.Success(context.Background(), "x") // must not panic
}

func TestAFailingWebhookNeverPanicsOrBlocks(t *testing.T) {
	// A notification must never fail or mask a backup outcome.
	rec := &recorder{respCode: http.StatusInternalServerError}
	n := newWebhook(t, rec, config.WebhookNotify{Enabled: true, OnSuccess: true})
	n.Success(context.Background(), "ok")
	if rec.calls != 1 {
		t.Fatalf("calls = %d, want 1", rec.calls)
	}
}

func TestAnUnreachableWebhookIsSwallowed(t *testing.T) {
	n := New(config.Notify{Webhook: config.WebhookNotify{
		Enabled: true, OnFailure: true,
		// Port 0 on the loopback never accepts a connection.
		URL: "http://127.0.0.1:0/hook",
	}})
	n.Failure(context.Background(), "backup failed")
}

func TestWebhookTruncatesAVeryLongSummary(t *testing.T) {
	rec := &recorder{}
	n := newWebhook(t, rec, config.WebhookNotify{Enabled: true, OnSuccess: true})
	n.Success(context.Background(), strings.Repeat("x", 50_000))

	// truncate() appends an ellipsis, so the cap is the limit plus those bytes.
	p := rec.payload(t)
	if len(p.Summary) > 10_000+len("…") {
		t.Errorf("summary not truncated: %d bytes", len(p.Summary))
	}
	if len(p.Text) > 3_900+len("…") {
		t.Errorf("text not truncated: %d bytes", len(p.Text))
	}
	if !strings.HasSuffix(p.Summary, "…") {
		t.Error("a truncated summary should say it was truncated")
	}
}
