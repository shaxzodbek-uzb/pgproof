package status

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
)

var now = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

func entry(db string, ago time.Duration, verify string) Entry {
	return Entry{
		Database:    db,
		Destination: "r2",
		Stamp:       now.Add(-ago),
		Manifest: catalog.Manifest{
			Verify:     verify,
			SizeBytes:  1024,
			DurationMS: 4300,
		},
	}
}

func find(r Report, db string) DatabaseStatus {
	for _, d := range r.Databases {
		if d.Database == db {
			return d
		}
	}
	return DatabaseStatus{}
}

func TestHealthyWhenLatestBackupPassedVerification(t *testing.T) {
	r := Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifyPassed)}, 0, now)
	if got := find(r, "app").Health; got != HealthOK {
		t.Fatalf("health = %q, want %q", got, HealthOK)
	}
	if !r.OK() {
		t.Fatal("report should be OK")
	}
}

func TestAConfiguredDatabaseWithNoBackupsIsMissing(t *testing.T) {
	// The most important case to surface, and the one a plain listing cannot show.
	r := Build([]string{"app", "billing"}, []Entry{entry("app", time.Hour, catalog.VerifyPassed)}, 0, now)
	if got := find(r, "billing").Health; got != HealthMissing {
		t.Fatalf("health = %q, want %q", got, HealthMissing)
	}
	if r.OK() {
		t.Fatal("a missing backup must not report OK")
	}
}

func TestAnUnverifiedBackupIsNotHealthy(t *testing.T) {
	// "A backup you haven't restored is a hope, not a backup."
	r := Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifySkipped)}, 0, now)
	if got := find(r, "app").Health; got != HealthUnverif {
		t.Fatalf("health = %q, want %q", got, HealthUnverif)
	}
}

func TestAFailedVerificationOutranksStaleness(t *testing.T) {
	r := Build([]string{"app"}, []Entry{entry("app", 72*time.Hour, catalog.VerifyFailed)}, time.Hour, now)
	if got := find(r, "app").Health; got != HealthFailed {
		t.Fatalf("health = %q, want %q", got, HealthFailed)
	}
}

func TestStalenessIsOnlyCheckedWhenMaxAgeIsSet(t *testing.T) {
	entries := []Entry{entry("app", 72*time.Hour, catalog.VerifyPassed)}
	if got := find(Build([]string{"app"}, entries, 0, now), "app").Health; got != HealthOK {
		t.Fatalf("without --max-age health = %q, want %q", got, HealthOK)
	}
	if got := find(Build([]string{"app"}, entries, 26*time.Hour, now), "app").Health; got != HealthStale {
		t.Fatalf("with --max-age health = %q, want %q", got, HealthStale)
	}
}

func TestTheLatestBackupWins(t *testing.T) {
	r := Build([]string{"app"}, []Entry{
		entry("app", 48*time.Hour, catalog.VerifyPassed),
		entry("app", time.Hour, catalog.VerifyFailed),
		entry("app", 24*time.Hour, catalog.VerifyPassed),
	}, 0, now)
	db := find(r, "app")
	if db.Health != HealthFailed {
		t.Fatalf("health = %q, want %q", db.Health, HealthFailed)
	}
	if db.Backups != 3 {
		t.Fatalf("backups = %d, want 3", db.Backups)
	}
	if !db.LastBackup.Equal(now.Add(-time.Hour)) {
		t.Fatalf("last backup = %v", db.LastBackup)
	}
}

func TestLastVerifiedCanBeOlderThanLastBackup(t *testing.T) {
	// The distinction that matters: a recent backup that failed its restore test
	// does not mean you have a recent restorable backup.
	r := Build([]string{"app"}, []Entry{
		entry("app", time.Hour, catalog.VerifyFailed),
		entry("app", 24*time.Hour, catalog.VerifyPassed),
	}, 0, now)
	db := find(r, "app")
	if db.LastVerified == nil {
		t.Fatal("expected a last-verified timestamp")
	}
	if !db.LastVerified.Equal(now.Add(-24 * time.Hour)) {
		t.Fatalf("last verified = %v, want 24h ago", db.LastVerified)
	}
}

func TestOverallHealthIsTheWorstDatabase(t *testing.T) {
	r := Build([]string{"a", "b"}, []Entry{
		entry("a", time.Hour, catalog.VerifyPassed),
		entry("b", time.Hour, catalog.VerifyFailed),
	}, 0, now)
	if r.Health != HealthFailed {
		t.Fatalf("overall = %q, want %q", r.Health, HealthFailed)
	}
}

func TestABackupForAnUnconfiguredDatabaseStillAppears(t *testing.T) {
	// Removing a database from the config should not hide backups that still exist.
	r := Build(nil, []Entry{entry("orphan", time.Hour, catalog.VerifyPassed)}, 0, now)
	if find(r, "orphan").Backups != 1 {
		t.Fatal("expected the orphaned database to be reported")
	}
}

func TestDatabasesAreSortedForStableOutput(t *testing.T) {
	r := Build([]string{"zeta", "alpha"}, nil, 0, now)
	if r.Databases[0].Database != "alpha" {
		t.Fatalf("first = %q, want alpha", r.Databases[0].Database)
	}
}

// -- JSON ---------------------------------------------------------------------

func TestJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	r := Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifyPassed)}, 0, now)
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if back.Health != HealthOK || len(back.Databases) != 1 {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestJSONOmitsAbsentTimestamps(t *testing.T) {
	var buf bytes.Buffer
	if err := Build([]string{"app"}, nil, 0, now).WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "last_backup") {
		t.Fatal("a database with no backups must not report a last_backup")
	}
}

// -- Prometheus ---------------------------------------------------------------

func prom(t *testing.T, r Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := r.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPrometheusEmitsHelpAndTypeForEveryMetric(t *testing.T) {
	out := prom(t, Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifyPassed)}, 0, now))
	for _, m := range metrics {
		if !strings.Contains(out, "# HELP pgproof_"+m.name+" ") {
			t.Errorf("missing HELP for %s", m.name)
		}
		if !strings.Contains(out, "# TYPE pgproof_"+m.name+" gauge") {
			t.Errorf("missing TYPE for %s", m.name)
		}
	}
}

func TestPrometheusReportsVerificationAsOneOrZero(t *testing.T) {
	passed := prom(t, Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifyPassed)}, 0, now))
	if !strings.Contains(passed, `pgproof_backup_last_verified{database="app"} 1`) {
		t.Errorf("verified backup should report 1:\n%s", passed)
	}
	failed := prom(t, Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifyFailed)}, 0, now))
	if !strings.Contains(failed, `pgproof_backup_last_verified{database="app"} 0`) {
		t.Errorf("failed backup should report 0:\n%s", failed)
	}
}

func TestPrometheusOmitsTimestampsForADatabaseWithNoBackups(t *testing.T) {
	out := prom(t, Build([]string{"app"}, nil, 0, now))
	if strings.Contains(out, "pgproof_backup_last_success_timestamp_seconds{database=\"app\"}") {
		t.Fatal("a database with no backups must not report a success timestamp")
	}
	// It must still report the facts it does know, so the database is visible.
	if !strings.Contains(out, `pgproof_backup_count{database="app"} 0`) {
		t.Fatalf("expected a zero backup count:\n%s", out)
	}
	if !strings.Contains(out, `pgproof_backup_healthy{database="app"} 0`) {
		t.Fatalf("expected unhealthy:\n%s", out)
	}
}

func TestPrometheusAlwaysReportsUp(t *testing.T) {
	if !strings.Contains(prom(t, Build(nil, nil, 0, now)), "pgproof_up 1") {
		t.Fatal("expected pgproof_up")
	}
}

func TestPrometheusValuesAreNotInExponentNotation(t *testing.T) {
	// Some scrapers mishandle exponents; a unix timestamp is big enough to trigger
	// Go's default %v formatting into 1.7e+09.
	out := prom(t, Build([]string{"app"}, []Entry{entry("app", time.Hour, catalog.VerifyPassed)}, 0, now))
	if strings.Contains(out, "e+") {
		t.Fatalf("exponent notation in output:\n%s", out)
	}
	if !strings.Contains(out, "pgproof_backup_last_duration_seconds{database=\"app\"} 4.3") {
		t.Fatalf("duration should render as seconds:\n%s", out)
	}
}

func TestPrometheusEscapesLabelValues(t *testing.T) {
	out := prom(t, Build([]string{`we"ird`}, nil, 0, now))
	if !strings.Contains(out, `database="we\"ird"`) {
		t.Fatalf("label value not escaped:\n%s", out)
	}
}

func TestPrometheusEveryLineIsWellFormed(t *testing.T) {
	out := prom(t, Build([]string{"a", "b"}, []Entry{
		entry("a", time.Hour, catalog.VerifyPassed),
		entry("b", 30*time.Hour, catalog.VerifyFailed),
	}, 26*time.Hour, now))
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Count(line, " ") < 1 {
			t.Errorf("sample line has no value: %q", line)
		}
		value := line[strings.LastIndex(line, " ")+1:]
		if value == "" || strings.ContainsAny(value, `"{}`) {
			t.Errorf("malformed sample value in %q", line)
		}
	}
}
