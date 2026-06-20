// Package scheduler runs a job on a cron schedule until the context is
// cancelled. It is used by `pgproof run` so the tool can act as a long-lived
// systemd/Docker service without depending on system cron.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// Run executes job on the cron spec (standard 5-field) in timezone tz until ctx
// is cancelled, then waits for any in-flight job to finish. Overlapping runs are
// skipped and panics are recovered so one bad run never kills the scheduler.
func Run(ctx context.Context, spec, tz string, log *slog.Logger, job func(context.Context)) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", tz, err)
	}

	cl := &cronLogger{log: log}
	c := cron.New(
		cron.WithLocation(loc),
		cron.WithLogger(cl),
		cron.WithChain(cron.Recover(cl), cron.SkipIfStillRunning(cl)),
	)

	// The job runs on a context decoupled from the shutdown signal so that
	// c.Stop() below genuinely waits for an in-flight backup to finish (each
	// operation is still bounded by its own per-op timeout) instead of having
	// pg_dump SIGKILLed mid-stream the instant SIGTERM arrives.
	jobCtx := context.WithoutCancel(ctx)
	if _, err := c.AddFunc(spec, func() { job(jobCtx) }); err != nil {
		return fmt.Errorf("invalid cron spec %q: %w", spec, err)
	}

	c.Start()
	log.Info("scheduler started", "cron", spec, "timezone", tz)
	if e := c.Entries(); len(e) > 0 {
		log.Info("next run", "at", e[0].Next.Format(time.RFC3339))
	}

	<-ctx.Done()
	log.Info("shutting down scheduler; waiting for in-flight backups")
	<-c.Stop().Done()
	return nil
}

// cronLogger adapts slog to the cron.Logger interface.
type cronLogger struct{ log *slog.Logger }

func (c *cronLogger) Info(msg string, kv ...interface{}) { c.log.Debug("cron: "+msg, kv...) }
func (c *cronLogger) Error(err error, msg string, kv ...interface{}) {
	c.log.Error("cron: "+msg, append([]interface{}{"error", err}, kv...)...)
}
