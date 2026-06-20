// Package catalog defines how backup artifacts are named in a destination and
// the JSON manifest stored alongside each one. The manifest carries metadata
// only — never credentials — so it is safe to store unencrypted next to the
// (possibly encrypted) dump.
package catalog

import (
	"path"
	"regexp"
	"strings"
	"time"
)

// StampFormat is the UTC timestamp embedded in every artifact key. It is
// lexicographically sortable, so sorting keys sorts by time.
const StampFormat = "20060102T150405Z"

// VerifyStatus values for a manifest.
const (
	VerifyPassed  = "passed"
	VerifySkipped = "skipped"
	VerifyFailed  = "failed"
)

// Manifest is the sidecar metadata for one backup.
type Manifest struct {
	Tool       string     `json:"tool"`
	Version    string     `json:"version"`
	Database   string     `json:"database"`
	DBName     string     `json:"dbname"`
	Driver     string     `json:"driver"`
	Format     string     `json:"format"`
	Encrypted  bool       `json:"encrypted"`
	Artifact   string     `json:"artifact"`
	SizeBytes  int64      `json:"size_bytes"`
	SHA256     string     `json:"sha256"`
	CreatedAt  time.Time  `json:"created_at"`
	DurationMS int64      `json:"duration_ms"`
	Verify     string     `json:"verify"` // passed|skipped|failed
	VerifyNote string     `json:"verify_note,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

// VerifyMark is a short symbol for display.
func (m Manifest) VerifyMark() string {
	switch m.Verify {
	case VerifyPassed:
		return "verified"
	case VerifyFailed:
		return "FAILED"
	default:
		return "unverified"
	}
}

var stampRe = regexp.MustCompile(`(\d{8}T\d{6}Z)`)

// Keyer builds and parses artifact/manifest keys under a destination prefix.
type Keyer struct {
	Prefix string
}

// DBPrefix is the listing prefix for a single database's backups.
func (k Keyer) DBPrefix(dbName string) string {
	return join(k.Prefix, dbName) + "/"
}

// ArtifactKey returns the object key for a dump file.
func (k Keyer) ArtifactKey(dbName, stamp, ext string, encrypted bool) string {
	name := dbName + "-" + stamp + ext
	if encrypted {
		name += ".age"
	}
	return join(k.Prefix, dbName, name)
}

// ManifestKey returns the sidecar manifest key for a backup stamp.
func (k Keyer) ManifestKey(dbName, stamp string) string {
	return join(k.Prefix, dbName, dbName+"-"+stamp+".json")
}

// IsManifest reports whether a key is a manifest sidecar.
func IsManifest(key string) bool {
	return strings.HasSuffix(key, ".json")
}

// StampOf extracts the UTC timestamp embedded in a key.
func StampOf(key string) (time.Time, bool) {
	m := stampRe.FindString(path.Base(key))
	if m == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(StampFormat, m)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Stamp formats t as an artifact timestamp (UTC).
func Stamp(t time.Time) string {
	return t.UTC().Format(StampFormat)
}

func join(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "/")
}
