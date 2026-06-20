package dump

import (
	"fmt"
	"os"
	"strings"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// writePgpass writes a temporary PGPASSFILE (mode 0600) so the password is never
// passed on argv (where it would be visible in `ps`). Returns the file path and
// a cleanup func. The database field is wildcarded so the same file works for the
// dump, the admin connection and the throwaway verify database.
func writePgpass(db config.Database) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "pgproof-pgpass-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.Remove(f.Name()) }

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	line := fmt.Sprintf("%s:%d:*:%s:%s\n",
		escapePgpass(db.Host), db.Port, escapePgpass(db.User), escapePgpass(db.Password))
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return f.Name(), cleanup, nil
}

// escapePgpass escapes ':' and '\' which are field separators in .pgpass.
func escapePgpass(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	return s
}

// pgEnv returns the environment for a libpq client invocation.
func pgEnv(pgpassFile string, db config.Database) []string {
	env := append(os.Environ(),
		"PGPASSFILE="+pgpassFile,
	)
	if db.SSLMode != "" {
		env = append(env, "PGSSLMODE="+db.SSLMode)
	}
	return env
}

// writeMySQLDefaults writes a temporary --defaults-extra-file (mode 0600) holding
// the connection credentials so they never appear on argv.
func writeMySQLDefaults(db config.Database) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "pgproof-mysql-*.cnf")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.Remove(f.Name()) }
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	content := fmt.Sprintf("[client]\nhost=%s\nport=%d\nuser=%s\npassword=%s\n",
		db.Host, db.Port, db.User, mysqlQuote(db.Password))
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return f.Name(), cleanup, nil
}

// mysqlQuote wraps a password in double quotes, escaping backslashes and quotes,
// so special characters survive my.cnf parsing.
func mysqlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
