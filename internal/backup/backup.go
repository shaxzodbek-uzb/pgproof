package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/buildinfo"
	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/dump"
	"github.com/shaxzodbek-uzb/pgproof/internal/verify"
)

// DBResult is the outcome of backing up one database.
type DBResult struct {
	Database   string
	SizeBytes  int64
	Encrypted  bool
	Verify     string // passed | skipped | failed
	VerifyNote string
	Tables     int
	StoredTo   []string
	Warnings   []string
	Err        error
	Duration   time.Duration
}

// Summary aggregates a backup run.
type Summary struct {
	Results []DBResult
}

// Failed reports whether any database failed to back up or failed verification.
func (s Summary) Failed() bool {
	for _, r := range s.Results {
		if r.Err != nil || r.Verify == catalog.VerifyFailed {
			return true
		}
	}
	return false
}

// Text renders a human-readable multi-line summary.
func (s Summary) Text() string {
	var b strings.Builder
	for _, r := range s.Results {
		if r.Err != nil {
			fmt.Fprintf(&b, "• %s: FAILED — %v\n", r.Database, r.Err)
			continue
		}
		v := r.Verify
		if r.Verify == catalog.VerifyPassed {
			v = fmt.Sprintf("verified ✓ (%d tables)", r.Tables)
		} else if r.Verify == catalog.VerifyFailed {
			v = "VERIFY FAILED ✗ — " + r.VerifyNote
		}
		stored := strings.Join(r.StoredTo, ", ")
		fmt.Fprintf(&b, "• %s: %s, %s → [%s] in %s\n",
			r.Database, humanBytes(r.SizeBytes), v, stored, r.Duration.Round(time.Millisecond))
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "    ⚠ %s\n", w)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// BackupAll backs up the given databases (or all when only is empty), verifying
// and notifying per config. It returns an error when any database fails.
func (r *Runner) BackupAll(ctx context.Context, only []string) (Summary, error) {
	dbs, err := r.targetDatabases(only)
	if err != nil {
		return Summary{}, err
	}

	r.notifier.Start(ctx)

	var sum Summary
	for _, db := range dbs {
		sum.Results = append(sum.Results, r.backupOne(ctx, db))
	}

	text := sum.Text()
	if sum.Failed() {
		r.notifier.Failure(ctx, text)
		return sum, errors.New("one or more databases failed to back up or verify")
	}
	r.notifier.Success(ctx, text)
	return sum, nil
}

func (r *Runner) backupOne(ctx context.Context, db config.Database) DBResult {
	start := time.Now()
	createdAt := start.UTC()
	stamp := catalog.Stamp(createdAt)
	base := db.Name + "-" + stamp
	res := DBResult{Database: db.Name, Verify: catalog.VerifySkipped}

	runDir := filepath.Join(r.cfg.StagingDir, base)
	defer os.RemoveAll(runDir)

	log := r.log.With("database", db.Name, "stamp", stamp)
	log.Info("starting backup")

	// 1. Dump.
	dctx, cancel := r.withTimeout(ctx)
	dres, err := dump.Dump(dctx, db, runDir, base, r.timeout(), log)
	cancel()
	if err != nil {
		res.Err = fmt.Errorf("dump: %w", err)
		log.Error("dump failed", "error", err)
		return res
	}
	plaintextPath := dres.Path
	ext := dump.ArtifactExt(db)
	encrypted := r.enc.Enabled()
	res.Encrypted = encrypted

	// 2. Encrypt (optional).
	finalPath := plaintextPath
	if encrypted {
		finalPath = plaintextPath + ".age"
		if err := r.encryptFile(plaintextPath, finalPath); err != nil {
			res.Err = fmt.Errorf("encrypt: %w", err)
			return res
		}
	}
	if info, err := os.Stat(finalPath); err == nil {
		res.SizeBytes = info.Size()
	}
	sha, err := sha256File(finalPath)
	if err != nil {
		res.Err = fmt.Errorf("hash artifact: %w", err)
		return res
	}

	// 3. Upload the artifact to every destination.
	for _, d := range r.dests {
		key := d.keyer.ArtifactKey(db.Name, stamp, ext, encrypted)
		uctx, ucancel := r.withTimeout(ctx)
		err := putFile(uctx, d, key, finalPath)
		ucancel()
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: store failed: %v", d.cfg.Name, err))
			log.Warn("destination store failed", "destination", d.cfg.Name, "error", err)
			continue
		}
		res.StoredTo = append(res.StoredTo, d.cfg.Name)
	}
	if len(res.StoredTo) == 0 {
		res.Err = errors.New("every destination failed to store the backup")
		return res
	}

	// 4. Verify (the restore-test).
	if r.cfg.Verify.Enabled {
		rep, vErr := r.verifyBackup(ctx, db, stamp, ext, encrypted, plaintextPath)
		switch {
		case vErr != nil:
			res.Verify = catalog.VerifyFailed
			res.VerifyNote = vErr.Error()
			log.Error("verify could not run", "error", vErr)
		case !rep.OK:
			res.Verify = catalog.VerifyFailed
			res.VerifyNote = rep.Note
			log.Error("verify failed", "note", rep.Note)
		default:
			res.Verify = catalog.VerifyPassed
			res.VerifyNote = rep.Note
			res.Tables = rep.Tables
			log.Info("verify passed", "tables", rep.Tables)
		}
	}

	// 5. Write the manifest sidecar to every readable destination.
	res.Duration = time.Since(start)
	man := catalog.Manifest{
		Tool:       "pgproof",
		Version:    buildinfo.Version,
		Database:   db.Name,
		DBName:     db.DBName,
		Driver:     db.Driver,
		Format:     dres.Format,
		Encrypted:  encrypted,
		SizeBytes:  res.SizeBytes,
		SHA256:     sha,
		CreatedAt:  createdAt,
		DurationMS: res.Duration.Milliseconds(),
		Verify:     res.Verify,
		VerifyNote: res.VerifyNote,
	}
	if res.Verify == catalog.VerifyPassed {
		now := time.Now().UTC()
		man.VerifiedAt = &now
	}
	r.writeManifests(ctx, db, stamp, ext, encrypted, man, &res)

	log.Info("backup complete", "size", humanBytes(res.SizeBytes), "stored_to", res.StoredTo, "verify", res.Verify)
	return res
}

func (r *Runner) writeManifests(ctx context.Context, db config.Database, stamp, ext string, encrypted bool, man catalog.Manifest, res *DBResult) {
	stored := make(map[string]bool, len(res.StoredTo))
	for _, name := range res.StoredTo {
		stored[name] = true
	}
	for _, d := range r.dests {
		if !d.d.Readable() {
			continue // sidecars are only useful where we can list/read them back
		}
		if !stored[d.cfg.Name] {
			continue // never advertise a backup on a destination that failed to store it
		}
		m := man
		m.Artifact = d.keyer.ArtifactKey(db.Name, stamp, ext, encrypted)
		payload, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			continue
		}
		key := d.keyer.ManifestKey(db.Name, stamp)
		mctx, cancel := r.withTimeout(ctx)
		err = d.d.Put(mctx, key, strings.NewReader(string(payload)), int64(len(payload)))
		cancel()
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: manifest write failed: %v", d.cfg.Name, err))
		}
	}
}

func (r *Runner) encryptFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = r.enc.Encrypt(out, in)
	return err
}

// verifyBackup runs the restore-test, sourcing the dump locally or, when
// verify.from_remote is set, by downloading and decrypting the stored artifact.
func (r *Runner) verifyBackup(ctx context.Context, db config.Database, stamp, ext string, encrypted bool, plaintextPath string) (verify.Report, error) {
	dumpPath := plaintextPath
	cleanup := func() {}

	if r.cfg.Verify.FromRemote {
		pd, err := r.restoreDest("")
		if err != nil {
			return verify.Report{}, err
		}
		key := pd.keyer.ArtifactKey(db.Name, stamp, ext, encrypted)
		p, c, err := r.materialize(ctx, pd, key, ext, encrypted)
		if err != nil {
			return verify.Report{}, fmt.Errorf("fetch stored artifact: %w", err)
		}
		dumpPath, cleanup = p, c
	}
	defer cleanup()

	vctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return verify.Run(vctx, verify.Target{
		DB:             db,
		DumpPath:       dumpPath,
		MinTables:      r.cfg.Verify.MinTables,
		RowCountTables: r.cfg.Verify.RowCountTables,
	}, r.log)
}

// materialize downloads (and decrypts) an artifact to a local plaintext file.
func (r *Runner) materialize(ctx context.Context, d dest, key, ext string, encrypted bool) (string, func(), error) {
	gctx, cancel := r.withTimeout(ctx)
	rc, err := d.d.Get(gctx, key)
	if err != nil {
		cancel()
		return "", func() {}, err
	}
	defer func() { _ = rc.Close(); cancel() }()

	tmp, err := os.CreateTemp("", "pgproof-verify-*"+ext)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }

	if encrypted {
		_, err = r.enc.Decrypt(tmp, rc)
	} else {
		_, err = io.Copy(tmp, rc)
	}
	if cerr := tmp.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp.Name(), cleanup, nil
}
