package dump

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

func dumpPostgres(ctx context.Context, db config.Database, out string, timeout time.Duration, log *slog.Logger) (Result, error) {
	dir, err := findPostgresDir(db.DumpPath)
	if err != nil {
		return Result{}, err
	}
	pgDump := filepath.Join(dir, exe("pg_dump"))

	pgpass, cleanup, err := writePgpass(db)
	if err != nil {
		return Result{}, fmt.Errorf("write pgpass: %w", err)
	}
	defer cleanup()

	args := []string{
		"--host", db.Host,
		"--port", strconv.Itoa(db.Port),
		"--username", db.User,
		"--dbname", db.DBName,
		"--no-owner", "--no-acl",
		"--no-password", // never prompt; creds come from PGPASSFILE
		"--compress", "6",
		"--file", out,
	}
	if db.DumpFormat == config.FormatCustom {
		args = append(args, "--format", "custom")
		if db.Jobs > 1 {
			// Parallel dump requires directory format; custom is single-stream.
			// Keep custom (portable single file) and let restore parallelise.
			log.Debug("pg_dump custom format is single-stream; jobs apply at restore time", "jobs", db.Jobs)
		}
	} else {
		args = append(args, "--format", "plain")
	}

	var stderr bytes.Buffer
	cmd := exec.Command(pgDump, args...)
	cmd.Env = pgEnv(pgpass, db)
	cmd.Stderr = &stderr

	log.Debug("running pg_dump", "bin", pgDump, "db", db.DBName, "format", db.DumpFormat, "out", out)
	if err := runDump(ctx, cmd, timeout, "pg_dump"); err != nil {
		return Result{}, fmt.Errorf("%w: %s", err, trimStderr(stderr.String()))
	}

	return Result{Path: out, Driver: db.Driver, Format: db.DumpFormat, Bytes: statSize(out)}, nil
}

func trimStderr(s string) string {
	const max = 2000
	if len(s) > max {
		return "..." + s[len(s)-max:]
	}
	return s
}
