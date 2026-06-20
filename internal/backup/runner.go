// Package backup is the orchestration core: it ties together dumping,
// encryption, upload to destinations, the restore-test, retention and
// notifications behind a small Runner API used by the CLI.
package backup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/crypto"
	"github.com/shaxzodbek-uzb/pgproof/internal/destination"
	"github.com/shaxzodbek-uzb/pgproof/internal/notify"
)

// Runner executes backup-related operations against a loaded config.
type Runner struct {
	cfg      *config.Config
	log      *slog.Logger
	enc      crypto.Scheme
	notifier *notify.Notifier
	dests    []dest
}

// dest pairs a live destination with the config needed to build object keys.
type dest struct {
	cfg   config.Destination
	d     destination.Destination
	keyer catalog.Keyer
}

// NewRunner builds a Runner, constructing every configured destination.
func NewRunner(cfg *config.Config, log *slog.Logger) (*Runner, error) {
	var enc crypto.Scheme
	if cfg.Encryption.Enabled {
		enc = crypto.Scheme{
			Passphrase: cfg.Encryption.Passphrase,
			Recipients: cfg.Encryption.Recipients,
			Identity:   cfg.Encryption.Identity,
		}
	}

	dests := make([]dest, 0, len(cfg.Destinations))
	for _, dc := range cfg.Destinations {
		d, err := destination.New(dc)
		if err != nil {
			return nil, fmt.Errorf("destination %q: %w", dc.Name, err)
		}
		dests = append(dests, dest{cfg: dc, d: d, keyer: catalog.Keyer{Prefix: dc.Prefix}})
	}

	return &Runner{
		cfg:      cfg,
		log:      log,
		enc:      enc,
		notifier: notify.New(cfg.Notify),
		dests:    dests,
	}, nil
}

// Config exposes the loaded config (read-only use by the CLI).
func (r *Runner) Config() *config.Config { return r.cfg }

// timeout is the per-operation timeout from config.
func (r *Runner) timeout() time.Duration {
	return time.Duration(r.cfg.TimeoutSeconds) * time.Second
}

// targetDatabases resolves the databases to operate on. An empty `only` means
// all configured databases.
func (r *Runner) targetDatabases(only []string) ([]config.Database, error) {
	if len(only) == 0 {
		return r.cfg.Databases, nil
	}
	var out []config.Database
	for _, name := range only {
		db, ok := r.cfg.DatabaseByName(name)
		if !ok {
			return nil, fmt.Errorf("no database named %q in config", name)
		}
		out = append(out, db)
	}
	return out, nil
}

// restoreDest returns the destination to read artifacts back from: the named one
// if given, else the first readable destination.
func (r *Runner) restoreDest(name string) (dest, error) {
	if name != "" {
		for _, d := range r.dests {
			if d.cfg.Name == name {
				if !d.d.Readable() {
					return dest{}, fmt.Errorf("destination %q is write-only and cannot be read from", name)
				}
				return d, nil
			}
		}
		return dest{}, fmt.Errorf("no destination named %q", name)
	}
	for _, d := range r.dests {
		if d.d.Readable() {
			return d, nil
		}
	}
	return dest{}, fmt.Errorf("no readable (s3/local) destination configured")
}

// withTimeout derives a context bounded by the configured timeout.
func (r *Runner) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout())
}
