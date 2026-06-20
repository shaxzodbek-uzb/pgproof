package verify

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/dump"
)

func runPostgres(ctx context.Context, t Target, temp string, log *slog.Logger) (Report, error) {
	db := t.DB
	adminDB := db.AdminDB
	if adminDB == "" {
		adminDB = "postgres"
	}

	dir, err := dump.PostgresDir(db.DumpPath)
	if err != nil {
		return Report{}, err
	}
	psql := dump.Tool(dir, "psql")
	pgRestore := dump.Tool(dir, "pg_restore")

	pgpass, cleanup, err := dump.WritePgpass(db)
	if err != nil {
		return Report{}, fmt.Errorf("write pgpass: %w", err)
	}
	defer cleanup()
	env := dump.PgEnv(pgpass, db)

	// 1. Create the throwaway database.
	if _, err := runPsql(ctx, psql, env, db, adminDB, true, `CREATE DATABASE "`+temp+`"`); err != nil {
		return Report{}, fmt.Errorf("create verify database: %w", err)
	}
	// Always drop it, even if a later step fails or the parent ctx was cancelled.
	defer func() {
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer dcancel()
		if _, derr := runPsql(dctx, psql, env, db, adminDB, false, `DROP DATABASE IF EXISTS "`+temp+`"`); derr != nil {
			log.Warn("could not drop verify database; remove it manually", "database", temp, "error", derr)
		}
	}()

	// 2. Restore the dump into the throwaway database.
	var restoreNote string
	if db.DumpFormat == config.FormatCustom {
		// pg_restore can exit non-zero on benign issues; the sanity checks below
		// are the verdict (a truly broken custom archive yields too few tables).
		restoreNote = restorePgCustom(ctx, pgRestore, env, db, temp, t.DumpPath, log)
	} else {
		// Plain restore runs psql without ON_ERROR_STOP, so a hard process error
		// — including a truncated/corrupt .sql.gz surfaced through the stdin copy
		// — means the artifact is NOT restorable. That is exactly what verify
		// must catch, so treat it as a failure rather than a warning.
		note, rerr := restorePgPlain(ctx, psql, env, db, temp, t.DumpPath, log)
		if rerr != nil {
			return Report{OK: false, Note: joinNote(note, "restore failed: "+rerr.Error())}, nil
		}
		restoreNote = note
	}

	// 3. Sanity-check the restored database.
	tables, err := pgCountTables(ctx, psql, env, db, temp)
	if err != nil {
		return Report{OK: false, Note: joinNote(restoreNote, "table count query failed: "+err.Error())}, nil
	}
	if tables < t.MinTables {
		return Report{OK: false, Tables: tables, Note: joinNote(restoreNote,
			fmt.Sprintf("restored %d tables, expected at least %d", tables, t.MinTables))}, nil
	}

	for _, tbl := range t.RowCountTables {
		n, err := pgCountRows(ctx, psql, env, db, temp, tbl)
		if err != nil {
			return Report{OK: false, Tables: tables, Note: joinNote(restoreNote,
				fmt.Sprintf("row-count check on %q failed: %v", tbl, err))}, nil
		}
		if n == 0 {
			return Report{OK: false, Tables: tables, Note: joinNote(restoreNote,
				fmt.Sprintf("table %q restored with 0 rows", tbl))}, nil
		}
	}

	note := fmt.Sprintf("restored %d tables into a throwaway database", tables)
	return Report{OK: true, Tables: tables, Note: joinNote(restoreNote, note)}, nil
}

func restorePgCustom(ctx context.Context, pgRestore string, env []string, db config.Database, temp, dumpPath string, log *slog.Logger) string {
	args := append(pgConnArgs(db, temp), "--no-owner", "--no-acl")
	if db.Jobs > 1 {
		args = append(args, "-j", strconv.Itoa(db.Jobs))
	}
	args = append(args, dumpPath)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, pgRestore, args...)
	cmd.Env = env
	cmd.Stderr = &stderr
	log.Debug("pg_restore into verify db", "db", temp)
	// pg_restore can exit non-zero on benign issues; the post-restore sanity
	// checks are the real verdict. We only surface stderr as a note.
	if err := cmd.Run(); err != nil {
		return "pg_restore warnings: " + dump.TrimStderr(stderr.String())
	}
	return ""
}

// restorePgPlain restores a gzipped plain SQL dump. It returns any stderr as a
// note and a non-nil error when the restore process itself failed (corrupt dump
// or a connection problem).
func restorePgPlain(ctx context.Context, psql string, env []string, db config.Database, temp, dumpPath string, log *slog.Logger) (string, error) {
	f, err := os.Open(dumpPath)
	if err != nil {
		return "", fmt.Errorf("open dump: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("not a valid gzip dump: %w", err)
	}
	defer gz.Close()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, psql, append(pgConnArgs(db, temp), "-q")...)
	cmd.Env = env
	cmd.Stdin = gz
	cmd.Stderr = &stderr
	log.Debug("psql restore into verify db", "db", temp)
	if err := cmd.Run(); err != nil {
		return dump.TrimStderr(stderr.String()), fmt.Errorf("psql: %w", err)
	}
	return "", nil
}

func pgCountTables(ctx context.Context, psql string, env []string, db config.Database, temp string) (int, error) {
	out, err := runPsql(ctx, psql, env, db, temp, true,
		"SELECT count(*) FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema NOT IN ('pg_catalog','information_schema')")
	if err != nil {
		return 0, err
	}
	return parseCount(out)
}

func pgCountRows(ctx context.Context, psql string, env []string, db config.Database, temp, table string) (int, error) {
	out, err := runPsql(ctx, psql, env, db, temp, true, "SELECT count(*) FROM "+quotePgIdent(table))
	if err != nil {
		return 0, err
	}
	return parseCount(out)
}

// quotePgIdent double-quotes a (optionally schema-qualified) identifier so it is
// injection-safe and case-exact. Inputs are already format-validated at config
// load (see config.Validate), this is defence in depth.
func quotePgIdent(name string) string {
	parts := strings.SplitN(name, ".", 2)
	for i, p := range parts {
		parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func pgConnArgs(db config.Database, targetDB string) []string {
	return []string{
		"-h", db.Host,
		"-p", strconv.Itoa(db.Port),
		"-U", db.User,
		"-d", targetDB,
		"--no-password",
	}
}

// runPsql runs a single SQL statement and returns trimmed unaligned output.
func runPsql(ctx context.Context, psql string, env []string, db config.Database, targetDB string, stopOnError bool, sql string) (string, error) {
	args := pgConnArgs(db, targetDB)
	args = append(args, "-X", "-q", "-t", "-A")
	if stopOnError {
		args = append(args, "-v", "ON_ERROR_STOP=1")
	}
	args = append(args, "-c", sql)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, psql, args...)
	cmd.Env = env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, dump.TrimStderr(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func parseCount(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("unexpected count output %q", s)
	}
	return n, nil
}

func joinNote(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "; ")
}
