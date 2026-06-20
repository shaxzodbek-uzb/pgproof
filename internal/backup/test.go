package backup

import (
	"context"

	"github.com/shaxzodbek-uzb/pgproof/internal/destination"
)

// TestResult is the connectivity check for one destination.
type TestResult struct {
	Name string
	Type string
	Err  error
}

// TestDestinations checks connectivity/credentials for every destination.
func (r *Runner) TestDestinations(ctx context.Context) []TestResult {
	out := make([]TestResult, 0, len(r.dests))
	for _, d := range r.dests {
		res := TestResult{Name: d.cfg.Name, Type: d.cfg.Type}
		if t, ok := d.d.(destination.Tester); ok {
			tctx, cancel := r.withTimeout(ctx)
			res.Err = t.Test(tctx)
			cancel()
		}
		out = append(out, res)
	}
	return out
}
