package retention

import (
	"testing"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

func items(times ...time.Time) []Item {
	out := make([]Item, len(times))
	for i, t := range times {
		out[i] = Item{ID: t.Format(time.RFC3339), Time: t}
	}
	return out
}

func ids(in []Item) map[string]bool {
	m := map[string]bool{}
	for _, it := range in {
		m[it.ID] = true
	}
	return m
}

func TestEmptyPolicyKeepsEverything(t *testing.T) {
	day := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	in := items(day, day.AddDate(0, 0, -1), day.AddDate(0, 0, -2))
	keep, remove := Plan(in, config.Retention{})
	if len(keep) != 3 || len(remove) != 0 {
		t.Fatalf("empty policy: keep=%d remove=%d, want 3/0", len(keep), len(remove))
	}
}

func TestKeepLastAndDaily(t *testing.T) {
	base := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	d5, d4, d3, d2, d1 := base, base.AddDate(0, 0, -1), base.AddDate(0, 0, -2), base.AddDate(0, 0, -3), base.AddDate(0, 0, -4)
	in := items(d5, d4, d3, d2, d1)

	keep, remove := Plan(in, config.Retention{KeepLast: 2, KeepDaily: 3})
	if len(keep) != 3 {
		t.Fatalf("keep=%d, want 3", len(keep))
	}
	k := ids(keep)
	for _, want := range []time.Time{d5, d4, d3} {
		if !k[want.Format(time.RFC3339)] {
			t.Errorf("expected %v kept", want)
		}
	}
	r := ids(remove)
	for _, want := range []time.Time{d2, d1} {
		if !r[want.Format(time.RFC3339)] {
			t.Errorf("expected %v removed", want)
		}
	}
}

func TestDailyKeepsNewestOfDay(t *testing.T) {
	morning := time.Date(2026, 6, 20, 6, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 6, 20, 20, 0, 0, 0, time.UTC)
	in := items(evening, morning)

	keep, remove := Plan(in, config.Retention{KeepDaily: 1})
	if len(keep) != 1 || len(remove) != 1 {
		t.Fatalf("keep=%d remove=%d, want 1/1", len(keep), len(remove))
	}
	if keep[0].Time != evening {
		t.Errorf("kept %v, want newest-of-day %v", keep[0].Time, evening)
	}
}

func TestMonthlyAndWeekly(t *testing.T) {
	base := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	// one per month for 4 months
	in := items(base, base.AddDate(0, -1, 0), base.AddDate(0, -2, 0), base.AddDate(0, -3, 0))
	keep, remove := Plan(in, config.Retention{KeepMonthly: 2})
	if len(keep) != 2 || len(remove) != 2 {
		t.Fatalf("monthly: keep=%d remove=%d, want 2/2", len(keep), len(remove))
	}
}
