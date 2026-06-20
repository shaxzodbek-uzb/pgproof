package destination

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// telegramDest is a write-only off-site copy: it pushes each artifact to a
// Telegram chat via the Bot API. Telegram bots cannot reliably list/fetch old
// uploads by name, so Get/List/Delete are unsupported — restore from an s3 or
// local destination instead.
type telegramDest struct {
	name     string
	token    string
	chatID   string
	maxBytes int64
	http     *http.Client
}

func newTelegram(cfg config.Destination) (Destination, error) {
	max := cfg.MaxSizeMB
	if max <= 0 {
		max = 50
	}
	return &telegramDest{
		name:     cfg.Name,
		token:    cfg.BotToken,
		chatID:   cfg.ChatID,
		maxBytes: int64(max) * 1024 * 1024,
		http:     &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (d *telegramDest) Name() string   { return d.name }
func (d *telegramDest) Type() string   { return config.TypeTelegram }
func (d *telegramDest) Readable() bool { return false }

func (d *telegramDest) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if size > d.maxBytes {
		return fmt.Errorf("telegram: artifact %s (%d bytes) exceeds the %d MB bot limit; use s3/local for this database",
			path.Base(key), size, d.maxBytes/1024/1024)
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// Build the request (and capture the multipart content type, fixed at
	// NewWriter time) BEFORE starting the writer goroutine, so an error here
	// can't leave the goroutine blocked forever on an unread pipe.
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", url.PathEscape(d.token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	go func() {
		err := func() error {
			if err := mw.WriteField("chat_id", d.chatID); err != nil {
				return err
			}
			part, err := mw.CreateFormFile("document", path.Base(key))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, r); err != nil {
				return err
			}
			return mw.Close()
		}()
		_ = pw.CloseWithError(err)
	}()

	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = json.Unmarshal(body, &out)
	if !out.OK {
		return fmt.Errorf("telegram API error (%d): %s", resp.StatusCode, out.Description)
	}
	return nil
}

func (d *telegramDest) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("telegram is write-only: %w", ErrUnsupported)
}

func (d *telegramDest) List(context.Context, string) ([]Artifact, error) {
	return nil, fmt.Errorf("telegram is write-only: %w", ErrUnsupported)
}

func (d *telegramDest) Delete(context.Context, string) error {
	return fmt.Errorf("telegram is write-only: %w", ErrUnsupported)
}

// Test verifies the bot token via getMe.
func (d *telegramDest) Test(ctx context.Context) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", url.PathEscape(d.token))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = json.Unmarshal(body, &out)
	if !out.OK {
		return fmt.Errorf("telegram getMe failed (%d): %s", resp.StatusCode, out.Description)
	}
	return nil
}

// Tester is implemented by destinations that can self-check connectivity.
type Tester interface {
	Test(ctx context.Context) error
}
