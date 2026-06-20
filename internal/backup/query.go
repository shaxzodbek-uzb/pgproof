package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/verify"
)

// manifestEntry is one backup as recorded by its manifest sidecar.
type manifestEntry struct {
	Stamp       time.Time
	ManifestKey string
	Manifest    catalog.Manifest
}

// ListEntry is a backup surfaced by the `list` command.
type ListEntry struct {
	Database    string
	Destination string
	Stamp       time.Time
	Manifest    catalog.Manifest
}

// List returns backups for a database (or all) from a destination (or the first
// readable one), newest first.
func (r *Runner) List(ctx context.Context, dbName, destName string) ([]ListEntry, error) {
	d, err := r.restoreDest(destName)
	if err != nil {
		return nil, err
	}
	dbNames, err := r.dbNames(dbName)
	if err != nil {
		return nil, err
	}

	var out []ListEntry
	for _, name := range dbNames {
		entries, err := r.listManifests(ctx, d, name)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			out = append(out, ListEntry{Database: name, Destination: d.cfg.Name, Stamp: e.Stamp, Manifest: e.Manifest})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stamp.After(out[j].Stamp) })
	return out, nil
}

// VerifyExisting re-runs the restore-test against an already-stored backup.
func (r *Runner) VerifyExisting(ctx context.Context, dbName, destName, which string) (verify.Report, error) {
	db, ok := r.cfg.DatabaseByName(dbName)
	if !ok {
		return verify.Report{}, fmt.Errorf("no database named %q in config", dbName)
	}
	d, err := r.restoreDest(destName)
	if err != nil {
		return verify.Report{}, err
	}
	entries, err := r.listManifests(ctx, d, dbName)
	if err != nil {
		return verify.Report{}, err
	}
	e, err := resolveEntry(entries, which)
	if err != nil {
		return verify.Report{}, err
	}

	ext := extFor(db.Driver, e.Manifest.Format)
	path, cleanup, err := r.materialize(ctx, d, e.Manifest.Artifact, ext, e.Manifest.Encrypted)
	if err != nil {
		return verify.Report{}, fmt.Errorf("fetch artifact: %w", err)
	}
	defer cleanup()

	vctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return verify.Run(vctx, verify.Target{
		DB:             db,
		DumpPath:       path,
		MinTables:      r.cfg.Verify.MinTables,
		RowCountTables: r.cfg.Verify.RowCountTables,
	}, r.log)
}

func (r *Runner) listManifests(ctx context.Context, d dest, dbName string) ([]manifestEntry, error) {
	lctx, cancel := r.withTimeout(ctx)
	defer cancel()
	arts, err := d.d.List(lctx, d.keyer.DBPrefix(dbName))
	if err != nil {
		return nil, fmt.Errorf("list %q on %s: %w", dbName, d.cfg.Name, err)
	}

	var entries []manifestEntry
	for _, a := range arts {
		if !catalog.IsManifest(a.Key) {
			continue
		}
		stamp, ok := catalog.StampOf(a.Key)
		if !ok {
			continue
		}
		m, err := r.readManifest(ctx, d, a.Key)
		if err != nil {
			r.log.Warn("skipping unreadable manifest", "key", a.Key, "error", err)
			continue
		}
		entries = append(entries, manifestEntry{Stamp: stamp, ManifestKey: a.Key, Manifest: m})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Stamp.After(entries[j].Stamp) })
	return entries, nil
}

func (r *Runner) readManifest(ctx context.Context, d dest, key string) (catalog.Manifest, error) {
	gctx, cancel := r.withTimeout(ctx)
	defer cancel()
	rc, err := d.d.Get(gctx, key)
	if err != nil {
		return catalog.Manifest{}, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return catalog.Manifest{}, err
	}
	var m catalog.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return catalog.Manifest{}, err
	}
	return m, nil
}

// dbNames resolves either a single named database or all configured names.
func (r *Runner) dbNames(only string) ([]string, error) {
	if only != "" {
		if _, ok := r.cfg.DatabaseByName(only); !ok {
			return nil, fmt.Errorf("no database named %q in config", only)
		}
		return []string{only}, nil
	}
	names := make([]string, 0, len(r.cfg.Databases))
	for _, db := range r.cfg.Databases {
		names = append(names, db.Name)
	}
	return names, nil
}

// resolveEntry picks the entry matching which ("" or "latest" → newest).
func resolveEntry(entries []manifestEntry, which string) (manifestEntry, error) {
	if len(entries) == 0 {
		return manifestEntry{}, fmt.Errorf("no backups found")
	}
	if which == "" || which == "latest" {
		return entries[0], nil // entries are newest-first
	}
	for _, e := range entries {
		if catalog.Stamp(e.Stamp) == which {
			return e, nil
		}
	}
	return manifestEntry{}, fmt.Errorf("no backup with id %q (use `pgproof list` to see ids)", which)
}

func extFor(driver, format string) string {
	if driver == config.DriverPostgres && format == config.FormatCustom {
		return ".dump"
	}
	return ".sql.gz"
}
