package backup

import (
	"context"
	"fmt"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/retention"
)

// PruneResult records what pruning did for one database on one destination.
type PruneResult struct {
	Database    string
	Destination string
	Kept        int
	Removed     []string // stamps removed (or that would be removed in dry-run)
	Errors      []string
}

// Prune applies the retention policy to the given databases (or all). With
// dryRun set, nothing is deleted — the result reports what would be removed.
func (r *Runner) Prune(ctx context.Context, only []string, dryRun bool) ([]PruneResult, error) {
	if !r.cfg.Retention.Any() {
		return nil, fmt.Errorf("no retention policy configured (set `retention.keep_*` in your config)")
	}
	dbs, err := r.targetDatabases(only)
	if err != nil {
		return nil, err
	}

	var results []PruneResult
	for _, db := range dbs {
		for _, d := range r.dests {
			if !d.d.Readable() {
				continue // cannot list/delete on write-only destinations
			}
			res := PruneResult{Database: db.Name, Destination: d.cfg.Name}
			entries, err := r.listManifests(ctx, d, db.Name)
			if err != nil {
				res.Errors = append(res.Errors, err.Error())
				results = append(results, res)
				continue
			}

			items := make([]retention.Item, len(entries))
			byID := make(map[string]manifestEntry, len(entries))
			for i, e := range entries {
				id := catalog.Stamp(e.Stamp)
				items[i] = retention.Item{ID: id, Time: e.Stamp}
				byID[id] = e
			}
			keep, remove := retention.Plan(items, r.cfg.Retention)
			res.Kept = len(keep)

			for _, it := range remove {
				res.Removed = append(res.Removed, it.ID)
				if dryRun {
					continue
				}
				e := byID[it.ID]
				if err := r.deleteBackup(ctx, d, e); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", it.ID, err))
				}
			}
			results = append(results, res)
		}
	}
	return results, nil
}

func (r *Runner) deleteBackup(ctx context.Context, d dest, e manifestEntry) error {
	dctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if e.Manifest.Artifact != "" {
		if err := d.d.Delete(dctx, e.Manifest.Artifact); err != nil {
			return fmt.Errorf("delete artifact: %w", err)
		}
	}
	if err := d.d.Delete(dctx, e.ManifestKey); err != nil {
		return fmt.Errorf("delete manifest: %w", err)
	}
	return nil
}
