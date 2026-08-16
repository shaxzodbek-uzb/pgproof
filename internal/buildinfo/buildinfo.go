// Package buildinfo carries version metadata stamped in at build time via
// -ldflags (see .goreleaser.yaml), falling back to the Go module metadata the
// toolchain embeds on its own.
//
// The fallback matters because `go install github.com/...@v0.2.0` — the way most
// people install a Go tool — does not apply the release ldflags. Without it the
// binary reports "dev" and cannot tell you which version you are running, which
// is a poor answer from a backup tool being cited in an incident report.
package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var (
	// Version is the semver tag (or "dev").
	Version = "dev"
	// Commit is the short git SHA.
	Commit = "none"
	// Date is the build timestamp.
	Date = "unknown"

	// dateIsCommitTime records that Date was recovered from VCS metadata rather
	// than stamped at build time. A `go install` build has no meaningful build
	// timestamp — the toolchain only knows when the commit was made — so the
	// banner says "commit dated" instead of claiming a build time it never had.
	dateIsCommitTime bool
)

func init() {
	// Only fill gaps. A value stamped by -ldflags is authoritative: a release
	// build knows its tag exactly, whereas module metadata reports "(devel)"
	// for anything built from a working tree.
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "none" && len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		case "vcs.time":
			if Date == "unknown" && s.Value != "" {
				Date = s.Value
				dateIsCommitTime = true
			}
		}
	}
}

// String renders a one-line version banner, naming only the fields it actually
// knows.
//
// Installing from the module proxy (`go install pkg@version`) gets no VCS
// metadata at all — the proxy serves a source archive, not a repository — so
// commit and date stay empty there. Printing "commit none, built unknown" in
// that case reads as a broken build rather than a normal install, so those
// parts are simply left out.
func String() string {
	s := "pgproof " + Version

	var parts []string
	if Commit != "none" {
		parts = append(parts, "commit "+Commit)
	}
	if Date != "unknown" {
		when := "built"
		if dateIsCommitTime {
			when = "commit dated"
		}
		parts = append(parts, when+" "+Date)
	}

	if len(parts) > 0 {
		s += fmt.Sprintf(" (%s)", strings.Join(parts, ", "))
	}

	return s
}
