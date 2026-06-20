// Package verify performs pgproof's headline feature: it proves a dump is
// actually restorable by restoring it into a throwaway database, running sanity
// checks, and dropping it again. The throwaway database is always cleaned up,
// even on failure.
package verify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// Target is one restore-test request operating on a local, decrypted dump file.
type Target struct {
	DB             config.Database
	DumpPath       string // local plaintext dump (.dump for custom, .sql.gz otherwise)
	MinTables      int
	RowCountTables []string
}

// Report is the outcome of a restore-test.
type Report struct {
	OK       bool
	Tables   int
	Note     string
	Duration time.Duration
}

// Run executes the restore-test for t.
func Run(ctx context.Context, t Target, log *slog.Logger) (Report, error) {
	if t.MinTables <= 0 {
		t.MinTables = 1
	}
	start := time.Now()
	temp := "pgproof_verify_" + randSuffix()

	var rep Report
	var err error
	switch t.DB.Driver {
	case config.DriverPostgres:
		rep, err = runPostgres(ctx, t, temp, log)
	case config.DriverMySQL:
		rep, err = runMySQL(ctx, t, temp, log)
	default:
		return Report{}, fmt.Errorf("verify: unsupported driver %q", t.DB.Driver)
	}
	rep.Duration = time.Since(start)
	return rep, err
}

func randSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// rand.Read essentially never fails; fall back to a time-based suffix.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
