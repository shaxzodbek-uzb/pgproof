// Package retention decides which backups to keep and which to prune using a
// grandfather-father-son policy (keep last N, plus N daily / weekly / monthly).
package retention

import (
	"sort"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// Item is a single backup considered for pruning.
type Item struct {
	ID   string // opaque identifier (e.g. timestamp string)
	Time time.Time
}

// Plan partitions items into keep and remove sets per the policy. When the
// policy is empty nothing is removed (a safety default: never prune by accident).
func Plan(items []Item, p config.Retention) (keep, remove []Item) {
	sorted := make([]Item, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.After(sorted[j].Time) })

	if !p.Any() {
		return sorted, nil
	}

	kept := make(map[string]bool)

	// keep_last: the N most recent regardless of spacing.
	for i := 0; i < p.KeepLast && i < len(sorted); i++ {
		kept[sorted[i].ID] = true
	}

	bucketKeep(sorted, kept, p.KeepDaily, func(t time.Time) string {
		return t.Format("2006-01-02")
	})
	bucketKeep(sorted, kept, p.KeepWeekly, func(t time.Time) string {
		y, w := t.ISOWeek()
		return isoKey(y, w)
	})
	bucketKeep(sorted, kept, p.KeepMonthly, func(t time.Time) string {
		return t.Format("2006-01")
	})

	for _, it := range sorted {
		if kept[it.ID] {
			keep = append(keep, it)
		} else {
			remove = append(remove, it)
		}
	}
	return keep, remove
}

// bucketKeep keeps the newest item in each of the first n distinct buckets.
func bucketKeep(sorted []Item, kept map[string]bool, n int, bucket func(time.Time) string) {
	if n <= 0 {
		return
	}
	seen := make(map[string]bool)
	for _, it := range sorted {
		if len(seen) >= n {
			break
		}
		b := bucket(it.Time.UTC())
		if seen[b] {
			continue
		}
		seen[b] = true
		kept[it.ID] = true
	}
}

func isoKey(year, week int) string {
	return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") + "-W" + pad2(week)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
