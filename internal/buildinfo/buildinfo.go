// Package buildinfo carries version metadata stamped in at build time via
// -ldflags (see .goreleaser.yaml).
package buildinfo

import "fmt"

var (
	// Version is the semver tag (or "dev").
	Version = "dev"
	// Commit is the short git SHA.
	Commit = "none"
	// Date is the build timestamp.
	Date = "unknown"
)

// String renders a one-line version banner.
func String() string {
	return fmt.Sprintf("pgproof %s (commit %s, built %s)", Version, Commit, Date)
}
