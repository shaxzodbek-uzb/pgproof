// Package destination abstracts where backups are stored. s3 (and S3-compatible
// services: Cloudflare R2, DigitalOcean Spaces, MinIO) and local filesystem are
// fully readable; telegram is a write-only off-site copy.
package destination

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// ErrUnsupported is returned by write-only destinations for read operations.
var ErrUnsupported = errors.New("operation not supported by this destination")

// Artifact is a stored object.
type Artifact struct {
	Key     string
	Size    int64
	ModTime time.Time
}

// Destination is a backup storage backend.
type Destination interface {
	Name() string
	Type() string
	// Readable reports whether Get/List/Delete are supported.
	Readable() bool
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Artifact, error)
	Delete(ctx context.Context, key string) error
}

// New builds a destination from its config.
func New(cfg config.Destination) (Destination, error) {
	switch cfg.Type {
	case config.TypeS3:
		return newS3(cfg)
	case config.TypeLocal:
		return newLocal(cfg)
	case config.TypeTelegram:
		return newTelegram(cfg)
	default:
		return nil, fmt.Errorf("unknown destination type %q", cfg.Type)
	}
}

// NewAll builds every configured destination.
func NewAll(cfgs []config.Destination) ([]Destination, error) {
	dests := make([]Destination, 0, len(cfgs))
	for _, c := range cfgs {
		d, err := New(c)
		if err != nil {
			return nil, fmt.Errorf("destination %q: %w", c.Name, err)
		}
		dests = append(dests, d)
	}
	return dests, nil
}
