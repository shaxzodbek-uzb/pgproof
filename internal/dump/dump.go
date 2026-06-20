// Package dump produces a compressed (and optionally custom-format) dump of a
// single database by shelling out to the native client tools (pg_dump /
// mysqldump). Credentials are passed via PGPASSFILE / --defaults-extra-file,
// never on argv.
package dump

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// Result describes a produced dump artifact on local disk.
type Result struct {
	Path   string // absolute path to the staged dump file
	Driver string
	Format string // custom | plain
	Bytes  int64
}

// ArtifactExt returns the file extension for a database's dump artifact.
func ArtifactExt(db config.Database) string {
	if db.Driver == config.DriverPostgres && db.DumpFormat == config.FormatCustom {
		return ".dump"
	}
	return ".sql.gz"
}

// Dump writes a dump of db into dir as <baseName><ext> and returns the result.
func Dump(ctx context.Context, db config.Database, dir, baseName string, timeout time.Duration, log *slog.Logger) (Result, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create staging dir: %w", err)
	}
	out := filepath.Join(dir, baseName+ArtifactExt(db))

	switch db.Driver {
	case config.DriverPostgres:
		return dumpPostgres(ctx, db, out, timeout, log)
	case config.DriverMySQL:
		return dumpMySQL(ctx, db, out, timeout, log)
	default:
		return Result{}, fmt.Errorf("unsupported driver %q", db.Driver)
	}
}

// runDump executes cmd with the per-dump timeout and surfaces stderr on failure.
func runDump(ctx context.Context, cmd *exec.Cmd, timeout time.Duration, what string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Re-bind the command to the timeout context.
	bound := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	bound.Env = cmd.Env
	bound.Stdout = cmd.Stdout
	bound.Stderr = cmd.Stderr
	bound.Dir = cmd.Dir

	if err := bound.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s timed out after %s", what, timeout)
		}
		return fmt.Errorf("%s failed: %w", what, err)
	}
	return nil
}

func statSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}
