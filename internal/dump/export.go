package dump

import (
	"path/filepath"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// Exported wrappers so the verify/restore packages can reuse the exact same
// client-binary discovery and credential handling used for dumping.

// PostgresDir resolves the directory holding pg_dump/pg_restore/psql.
func PostgresDir(configuredDir string) (string, error) {
	return findPostgresDir(configuredDir)
}

// Tool joins a client directory and a binary name (handling .exe on Windows).
func Tool(dir, name string) string {
	return filepath.Join(dir, exe(name))
}

// FindMySQLClient locates the `mysql` binary.
func FindMySQLClient(dir string) (string, error) {
	return findBinary("mysql", dir)
}

// WritePgpass exposes the temporary PGPASSFILE writer.
func WritePgpass(db config.Database) (path string, cleanup func(), err error) {
	return writePgpass(db)
}

// PgEnv exposes the libpq client environment builder.
func PgEnv(pgpassFile string, db config.Database) []string {
	return pgEnv(pgpassFile, db)
}

// WriteMySQLDefaults exposes the temporary my.cnf writer.
func WriteMySQLDefaults(db config.Database) (path string, cleanup func(), err error) {
	return writeMySQLDefaults(db)
}

// TrimStderr exposes stderr trimming for callers that shell out to clients.
func TrimStderr(s string) string { return trimStderr(s) }
