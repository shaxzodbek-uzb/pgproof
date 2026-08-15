// Package status summarises the latest backup per database and renders it for
// humans, for JSON, and as Prometheus text.
//
// A backup tool that can't be monitored is a backup tool you find out about on
// the morning your primary dies — which is the failure this project exists to
// prevent. Everything here derives from the manifests already stored beside each
// artifact, so there is no second source of truth to drift.
//
// Rendering the Prometheus exposition format by hand keeps the single static
// binary free of a metrics client dependency. The format is a stable, documented
// line protocol; it does not warrant one.
package status

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
)

// Health values, ordered worst-last so a run's overall health is a max.
const (
	HealthOK      = "ok"
	HealthStale   = "stale"
	HealthUnverif = "unverified"
	HealthFailed  = "failed"
	HealthMissing = "missing"
)

var healthRank = map[string]int{
	HealthOK: 0, HealthStale: 1, HealthUnverif: 2, HealthFailed: 3, HealthMissing: 4,
}

// Entry is the minimum this package needs from a stored backup, so it does not
// import (and cannot be broken by) the backup runner's own types.
type Entry struct {
	Database    string
	Destination string
	Stamp       time.Time
	Manifest    catalog.Manifest
}

// DatabaseStatus is the latest known state of one database's backups.
type DatabaseStatus struct {
	Database    string     `json:"database"`
	Destination string     `json:"destination,omitempty"`
	Health      string     `json:"health"`
	Backups     int        `json:"backups"`
	LastBackup  *time.Time `json:"last_backup,omitempty"`
	AgeSeconds  *float64   `json:"age_seconds,omitempty"`
	SizeBytes   int64      `json:"size_bytes,omitempty"`
	DurationMS  int64      `json:"duration_ms,omitempty"`
	Verify      string     `json:"verify,omitempty"`
	VerifyNote  string     `json:"verify_note,omitempty"`
	Encrypted   bool       `json:"encrypted,omitempty"`
	// LastVerified is the newest backup that actually passed a restore test,
	// which can be older than LastBackup — the distinction that matters.
	LastVerified *time.Time `json:"last_verified,omitempty"`
}

// Report is the whole picture at one instant.
type Report struct {
	GeneratedAt time.Time        `json:"generated_at"`
	MaxAge      string           `json:"max_age,omitempty"`
	Health      string           `json:"health"`
	Databases   []DatabaseStatus `json:"databases"`
}

// OK reports whether every database is healthy.
func (r Report) OK() bool { return r.Health == HealthOK }

// Build reduces a backup listing to one status per database.
//
// databases is the configured set, so a database with no backups at all is
// reported as missing rather than silently omitted — the most important case to
// surface, and the one a listing alone cannot show.
//
// maxAge of zero disables the staleness check.
func Build(databases []string, entries []Entry, maxAge time.Duration, now time.Time) Report {
	byDB := make(map[string][]Entry, len(databases))
	for _, e := range entries {
		byDB[e.Database] = append(byDB[e.Database], e)
	}

	names := append([]string(nil), databases...)
	for name := range byDB {
		if !contains(names, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	report := Report{GeneratedAt: now, Health: HealthOK}
	if maxAge > 0 {
		report.MaxAge = maxAge.String()
	}
	for _, name := range names {
		st := buildOne(name, byDB[name], maxAge, now)
		report.Databases = append(report.Databases, st)
		if healthRank[st.Health] > healthRank[report.Health] {
			report.Health = st.Health
		}
	}
	return report
}

func buildOne(name string, entries []Entry, maxAge time.Duration, now time.Time) DatabaseStatus {
	st := DatabaseStatus{Database: name, Backups: len(entries)}
	if len(entries) == 0 {
		st.Health = HealthMissing
		return st
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Stamp.After(entries[j].Stamp) })
	latest := entries[0]
	stamp := latest.Stamp
	age := now.Sub(stamp).Seconds()

	st.Destination = latest.Destination
	st.LastBackup = &stamp
	st.AgeSeconds = &age
	st.SizeBytes = latest.Manifest.SizeBytes
	st.DurationMS = latest.Manifest.DurationMS
	st.Verify = latest.Manifest.Verify
	st.VerifyNote = latest.Manifest.VerifyNote
	st.Encrypted = latest.Manifest.Encrypted

	for _, e := range entries {
		if e.Manifest.Verify == catalog.VerifyPassed {
			verified := e.Stamp
			st.LastVerified = &verified
			break
		}
	}

	switch {
	case latest.Manifest.Verify == catalog.VerifyFailed:
		st.Health = HealthFailed
	case maxAge > 0 && now.Sub(stamp) > maxAge:
		st.Health = HealthStale
	case latest.Manifest.Verify != catalog.VerifyPassed:
		// A backup nobody has restore-tested is a hope, not a backup.
		st.Health = HealthUnverif
	default:
		st.Health = HealthOK
	}
	return st
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// WriteJSON renders the report as indented JSON.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// -- Prometheus ---------------------------------------------------------------

const metricPrefix = "pgproof_"

type metric struct {
	name string
	help string
	typ  string
}

var metrics = []metric{
	{"backup_last_success_timestamp_seconds", "Unix time of the most recent stored backup.", "gauge"},
	{"backup_last_verified_timestamp_seconds", "Unix time of the most recent backup that passed a restore test.", "gauge"},
	{"backup_last_age_seconds", "Age of the most recent stored backup.", "gauge"},
	{"backup_last_size_bytes", "Size of the most recent stored backup.", "gauge"},
	{"backup_last_duration_seconds", "How long the most recent backup took.", "gauge"},
	{"backup_last_verified", "1 if the most recent backup passed its restore test, else 0.", "gauge"},
	{"backup_count", "Number of stored backups for this database.", "gauge"},
	{"backup_healthy", "1 if this database's backups are healthy, else 0.", "gauge"},
}

// WritePrometheus renders the report in the Prometheus text exposition format.
//
// Only gauges derived from stored manifests: this is a snapshot of what is on
// disk, not a record of process activity. Counters would need state pgproof
// deliberately does not keep, and a counter that resets on every invocation is
// worse than no counter.
func (r Report) WritePrometheus(w io.Writer) error {
	var b strings.Builder

	for _, m := range metrics {
		fmt.Fprintf(&b, "# HELP %s%s %s\n", metricPrefix, m.name, m.help)
		fmt.Fprintf(&b, "# TYPE %s%s %s\n", metricPrefix, m.name, m.typ)
		for _, db := range r.Databases {
			value, ok := metricValue(m.name, db)
			if !ok {
				continue // a database with no backups has no timestamp to report
			}
			// %s with hand-escaped quotes, not %q: %q would escape the escapes.
			fmt.Fprintf(&b, "%s%s{database=\"%s\"} %s\n",
				metricPrefix, m.name, escapeLabel(db.Database), formatFloat(value))
		}
	}

	fmt.Fprintf(&b, "# HELP %sup 1 if the last status check completed.\n", metricPrefix)
	fmt.Fprintf(&b, "# TYPE %sup gauge\n", metricPrefix)
	fmt.Fprintf(&b, "%sup 1\n", metricPrefix)
	fmt.Fprintf(&b, "# HELP %sstatus_timestamp_seconds Unix time this status was generated.\n", metricPrefix)
	fmt.Fprintf(&b, "# TYPE %sstatus_timestamp_seconds gauge\n", metricPrefix)
	fmt.Fprintf(&b, "%sstatus_timestamp_seconds %s\n", metricPrefix, formatFloat(float64(r.GeneratedAt.Unix())))

	_, err := io.WriteString(w, b.String())
	return err
}

func metricValue(name string, db DatabaseStatus) (float64, bool) {
	switch name {
	case "backup_last_success_timestamp_seconds":
		if db.LastBackup == nil {
			return 0, false
		}
		return float64(db.LastBackup.Unix()), true
	case "backup_last_verified_timestamp_seconds":
		if db.LastVerified == nil {
			return 0, false
		}
		return float64(db.LastVerified.Unix()), true
	case "backup_last_age_seconds":
		if db.AgeSeconds == nil {
			return 0, false
		}
		return *db.AgeSeconds, true
	case "backup_last_size_bytes":
		if db.LastBackup == nil {
			return 0, false
		}
		return float64(db.SizeBytes), true
	case "backup_last_duration_seconds":
		if db.LastBackup == nil {
			return 0, false
		}
		return float64(db.DurationMS) / 1000.0, true
	case "backup_last_verified":
		if db.LastBackup == nil {
			return 0, false
		}
		return boolValue(db.Verify == catalog.VerifyPassed), true
	case "backup_count":
		return float64(db.Backups), true
	case "backup_healthy":
		return boolValue(db.Health == HealthOK), true
	}
	return 0, false
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// formatFloat renders without an exponent, which some scrapers mishandle, and
// without a trailing ".0" on whole numbers.
func formatFloat(f float64) string {
	s := fmt.Sprintf("%.6f", f)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// escapeLabel escapes a label value per the exposition format.
func escapeLabel(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(s)
}
