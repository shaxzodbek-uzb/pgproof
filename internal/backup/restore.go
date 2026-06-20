package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/dump"
)

// RestoreOptions parameterises a restore into a live database.
type RestoreOptions struct {
	Database string // config database name (provides connection + driver)
	Dest     string // destination to read from ("" → first readable)
	Which    string // "latest" or a backup id
	TargetDB string // database to restore INTO ("" → the configured dbname)
	Clean    bool   // pg_restore --clean --if-exists (custom format only)
}

// Restore downloads, decrypts and restores a stored backup into a live database.
func (r *Runner) Restore(ctx context.Context, opts RestoreOptions) error {
	db, ok := r.cfg.DatabaseByName(opts.Database)
	if !ok {
		return fmt.Errorf("no database named %q in config", opts.Database)
	}
	d, err := r.restoreDest(opts.Dest)
	if err != nil {
		return err
	}
	entries, err := r.listManifests(ctx, d, opts.Database)
	if err != nil {
		return err
	}
	e, err := resolveEntry(entries, opts.Which)
	if err != nil {
		return err
	}

	ext := extFor(db.Driver, e.Manifest.Format)
	path, cleanup, err := r.materialize(ctx, d, e.Manifest.Artifact, ext, e.Manifest.Encrypted)
	if err != nil {
		return fmt.Errorf("fetch artifact: %w", err)
	}
	defer cleanup()

	target := opts.TargetDB
	if target == "" {
		target = db.DBName
	}

	rctx, cancel := r.withTimeout(ctx)
	defer cancel()
	r.log.Info("restoring", "database", opts.Database, "into", target, "from", d.cfg.Name, "id", catalog.Stamp(e.Stamp))

	switch db.Driver {
	case config.DriverPostgres:
		return r.restorePostgres(rctx, db, target, e.Manifest.Format, opts.Clean, path)
	case config.DriverMySQL:
		return r.restoreMySQL(rctx, db, target, path)
	default:
		return fmt.Errorf("unsupported driver %q", db.Driver)
	}
}

func (r *Runner) restorePostgres(ctx context.Context, db config.Database, target, format string, clean bool, path string) error {
	dir, err := dump.PostgresDir(db.DumpPath)
	if err != nil {
		return err
	}
	pgpass, cleanup, err := dump.WritePgpass(db)
	if err != nil {
		return err
	}
	defer cleanup()
	env := dump.PgEnv(pgpass, db)

	conn := []string{"-h", db.Host, "-p", strconv.Itoa(db.Port), "-U", db.User, "-d", target, "--no-password"}

	if format == config.FormatCustom {
		args := append(conn, "--no-owner", "--no-acl")
		if clean {
			args = append(args, "--clean", "--if-exists")
		}
		if db.Jobs > 1 {
			args = append(args, "-j", strconv.Itoa(db.Jobs))
		}
		args = append(args, path)

		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, dump.Tool(dir, "pg_restore"), args...)
		cmd.Env = env
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pg_restore: %w: %s", err, dump.TrimStderr(stderr.String()))
		}
		return nil
	}

	// plain (.sql.gz) → psql
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, dump.Tool(dir, "psql"), append(conn, "-q", "-v", "ON_ERROR_STOP=1")...)
	cmd.Env = env
	cmd.Stdin = gz
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql restore: %w: %s", err, dump.TrimStderr(stderr.String()))
	}
	return nil
}

func (r *Runner) restoreMySQL(ctx context.Context, db config.Database, target, path string) error {
	bin, err := dump.FindMySQLClient(db.DumpPath)
	if err != nil {
		return err
	}
	defaults, cleanup, err := dump.WriteMySQLDefaults(db)
	if err != nil {
		return err
	}
	defer cleanup()

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "--defaults-extra-file="+defaults, target)
	cmd.Stdin = gz
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysql restore: %w: %s", err, dump.TrimStderr(stderr.String()))
	}
	return nil
}
