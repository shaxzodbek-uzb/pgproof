package dump

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

func dumpMySQL(ctx context.Context, db config.Database, out string, timeout time.Duration, log *slog.Logger) (Result, error) {
	bin, err := findBinary("mysqldump", db.DumpPath)
	if err != nil {
		return Result{}, err
	}
	defaults, cleanup, err := writeMySQLDefaults(db)
	if err != nil {
		return Result{}, fmt.Errorf("write mysql defaults: %w", err)
	}
	defer cleanup()

	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create dump file: %w", err)
	}
	gz := gzip.NewWriter(f)

	// Clean up the partial artifact on any failure path.
	success := false
	defer func() {
		_ = gz.Close()
		_ = f.Close()
		if !success {
			_ = os.Remove(out)
		}
	}()

	args := []string{
		"--defaults-extra-file=" + defaults,
		"--single-transaction", "--quick",
		"--routines", "--triggers", "--events",
		"--no-tablespaces",
		"--default-character-set=utf8mb4",
		db.DBName,
	}

	tctx, tcancel := context.WithTimeout(ctx, timeout)
	defer tcancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(tctx, bin, args...)
	cmd.Stdout = gz
	cmd.Stderr = &stderr

	log.Debug("running mysqldump", "bin", bin, "db", db.DBName, "out", out)
	if err := cmd.Run(); err != nil {
		if tctx.Err() == context.DeadlineExceeded {
			return Result{}, fmt.Errorf("mysqldump timed out after %s", timeout)
		}
		return Result{}, fmt.Errorf("mysqldump failed: %w: %s", err, trimStderr(stderr.String()))
	}
	if err := gz.Close(); err != nil {
		return Result{}, fmt.Errorf("finalize gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		return Result{}, fmt.Errorf("close dump file: %w", err)
	}
	success = true

	return Result{Path: out, Driver: db.Driver, Format: config.FormatPlain, Bytes: statSize(out)}, nil
}
