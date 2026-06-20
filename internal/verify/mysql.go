package verify

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/dump"
)

func runMySQL(ctx context.Context, t Target, temp string, log *slog.Logger) (Report, error) {
	db := t.DB
	bin, err := dump.FindMySQLClient(db.DumpPath)
	if err != nil {
		return Report{}, err
	}
	defaults, cleanup, err := dump.WriteMySQLDefaults(db)
	if err != nil {
		return Report{}, fmt.Errorf("write mysql defaults: %w", err)
	}
	defer cleanup()

	if _, err := mysqlExec(ctx, bin, defaults, "", "CREATE DATABASE `"+temp+"`"); err != nil {
		return Report{}, fmt.Errorf("create verify database: %w", err)
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer dcancel()
		if _, derr := mysqlExec(dctx, bin, defaults, "", "DROP DATABASE IF EXISTS `"+temp+"`"); derr != nil {
			log.Warn("could not drop verify database; remove it manually", "database", temp, "error", derr)
		}
	}()

	restoreNote, rerr := restoreMySQL(ctx, bin, defaults, temp, t.DumpPath, log)
	if rerr != nil {
		return Report{OK: false, Note: joinNote(restoreNote, "restore failed: "+rerr.Error())}, nil
	}

	tables, err := mysqlCountTables(ctx, bin, defaults, temp)
	if err != nil {
		return Report{OK: false, Note: joinNote(restoreNote, "table count query failed: "+err.Error())}, nil
	}
	if tables < t.MinTables {
		return Report{OK: false, Tables: tables, Note: joinNote(restoreNote,
			fmt.Sprintf("restored %d tables, expected at least %d", tables, t.MinTables))}, nil
	}

	for _, tbl := range t.RowCountTables {
		out, err := mysqlExec(ctx, bin, defaults, temp, "SELECT count(*) FROM "+quoteMyIdent(tbl))
		if err != nil {
			return Report{OK: false, Tables: tables, Note: joinNote(restoreNote,
				fmt.Sprintf("row-count check on %q failed: %v", tbl, err))}, nil
		}
		n, perr := parseCount(out)
		if perr != nil || n == 0 {
			return Report{OK: false, Tables: tables, Note: joinNote(restoreNote,
				fmt.Sprintf("table %q restored with 0 rows", tbl))}, nil
		}
	}

	return Report{OK: true, Tables: tables, Note: joinNote(restoreNote,
		fmt.Sprintf("restored %d tables into a throwaway database", tables))}, nil
}

// restoreMySQL restores a gzipped SQL dump. It returns any stderr as a note and
// a non-nil error when the restore process itself failed (corrupt dump or a
// connection problem).
func restoreMySQL(ctx context.Context, bin, defaults, temp, dumpPath string, log *slog.Logger) (string, error) {
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
	cmd := exec.CommandContext(ctx, bin, "--defaults-extra-file="+defaults, temp)
	cmd.Stdin = gz
	cmd.Stderr = &stderr
	log.Debug("mysql restore into verify db", "db", temp)
	if err := cmd.Run(); err != nil {
		return dump.TrimStderr(stderr.String()), fmt.Errorf("mysql: %w", err)
	}
	return "", nil
}

func mysqlCountTables(ctx context.Context, bin, defaults, temp string) (int, error) {
	out, err := mysqlExec(ctx, bin, defaults, "",
		"SELECT count(*) FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema='"+temp+"'")
	if err != nil {
		return 0, err
	}
	return parseCount(out)
}

// quoteMyIdent backtick-quotes a (optionally schema-qualified) identifier.
// Inputs are format-validated at config load; this is defence in depth.
func quoteMyIdent(name string) string {
	parts := strings.SplitN(name, ".", 2)
	for i, p := range parts {
		parts[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
	}
	return strings.Join(parts, ".")
}

// mysqlExec runs a single statement, optionally against a specific database.
func mysqlExec(ctx context.Context, bin, defaults, db, sql string) (string, error) {
	args := []string{"--defaults-extra-file=" + defaults, "-N", "-B"}
	if db != "" {
		args = append(args, db)
	}
	args = append(args, "-e", sql)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, dump.TrimStderr(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
