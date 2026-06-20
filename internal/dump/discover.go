package dump

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Default search locations for client binaries (pg_dump/pg_restore/psql). Ported
// from the proven db-backup-manager engine: a Postgres N server must be dumped
// by a client whose major version is >= N, so we consider every install root and
// pick the newest binary, falling back to PATH.
var (
	binaryGlobs = []string{
		"/Users/Shared/DBngin/postgresql/*",
		"/opt/homebrew/opt/postgresql@*",
		"/opt/homebrew/Cellar/postgresql@*/*",
		"/usr/local/opt/postgresql@*",
		"/usr/lib/postgresql/*",
		"/usr/pgsql-*",
		"/Applications/Postgres.app/Contents/Versions/*",
	}
	binarySearchPaths = []string{
		"/opt/homebrew/opt/libpq/bin",
		"/usr/local/opt/libpq/bin",
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
	}
)

var versionRe = regexp.MustCompile(`(\d+(?:\.\d+)*)`)

// findPostgresDir resolves the directory that holds pg_dump/pg_restore/psql,
// preferring an explicit dir, then the newest discovered install, then PATH.
func findPostgresDir(configuredDir string) (string, error) {
	if configuredDir != "" {
		if isExecutable(filepath.Join(configuredDir, exe("pg_dump"))) {
			return configuredDir, nil
		}
		return "", fmt.Errorf("pg_dump not found in configured dump_path %q", configuredDir)
	}

	best, bestVer := "", ""
	for _, bin := range candidatePostgresBinaries() {
		ver := binaryVersion(bin)
		if ver != "" && compareVersions(ver, bestVer) > 0 {
			best, bestVer = bin, ver
		}
	}
	if best != "" {
		return filepath.Dir(best), nil
	}

	if p, err := exec.LookPath("pg_dump"); err == nil {
		return filepath.Dir(p), nil
	}
	return "", fmt.Errorf("pg_dump was not found. Install the postgresql client " +
		"(e.g. `brew install libpq` / `apt-get install postgresql-client`) or set the database's `dump_path`")
}

func candidatePostgresBinaries() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] && isExecutable(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, g := range binaryGlobs {
		matches, _ := filepath.Glob(filepath.Join(g, "bin", exe("pg_dump")))
		for _, m := range matches {
			add(m)
		}
	}
	for _, d := range binarySearchPaths {
		add(filepath.Join(d, exe("pg_dump")))
	}
	return out
}

// findBinary locates a single binary in dir or on PATH (used for mysqldump/mysql).
func findBinary(name, dir string) (string, error) {
	if dir != "" {
		cand := filepath.Join(dir, exe(name))
		if isExecutable(cand) {
			return cand, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s was not found on PATH; install the database client or set `dump_path`", name)
}

func binaryVersion(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return ""
	}
	return m[1]
}

// compareVersions compares dotted version strings (e.g. "16.3" vs "17.0").
func compareVersions(a, b string) int {
	if b == "" {
		return 1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = atoi(as[i])
		}
		if i < len(bs) {
			y = atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
