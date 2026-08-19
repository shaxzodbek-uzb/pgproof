package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// seedUnreadable writes a backup whose stored artifact cannot be decrypted, and
// whose manifest still claims the backup passed its restore test — the state a
// backup leaves behind once its artifact rots in the destination.
func seedUnreadable(t *testing.T, dir string, stamp time.Time) string {
	t.Helper()
	k := catalog.Keyer{}
	s := catalog.Stamp(stamp)
	artKey := k.ArtifactKey("app", s, ".dump", true)
	manKey := k.ManifestKey("app", s)

	writeFile(t, filepath.Join(dir, artKey), []byte("this is not a valid age stream"))

	man := catalog.Manifest{
		Tool: "pgproof", Database: "app", DBName: "app", Driver: config.DriverPostgres,
		Format: config.FormatCustom, Encrypted: true, Artifact: artKey,
		SizeBytes: 30, CreatedAt: stamp, Verify: catalog.VerifyPassed,
		VerifyNote: "restored 5 tables into a throwaway database",
	}
	payload, _ := json.MarshalIndent(man, "", "  ")
	writeFile(t, filepath.Join(dir, manKey), payload)
	return manKey
}

// A failed verification must reach the manifest. Otherwise `status` and
// `metrics` keep reporting a backup as verified after the proof that it is not.
func TestVerifyExistingRecordsFailure(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Now().UTC().Truncate(time.Second)
	manKey := seedUnreadable(t, dir, stamp)

	cfg := testConfig(dir, config.Retention{})
	cfg.Encryption = config.Encryption{Enabled: true, Passphrase: "wrong-passphrase"}
	r, err := NewRunner(cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.VerifyExisting(context.Background(), "app", "", "latest"); err == nil {
		t.Fatal("VerifyExisting succeeded on an undecryptable artifact; want an error")
	}

	raw, err := os.ReadFile(filepath.Join(dir, manKey))
	if err != nil {
		t.Fatal(err)
	}
	var got catalog.Manifest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Verify != catalog.VerifyFailed {
		t.Errorf("manifest verify = %q, want %q", got.Verify, catalog.VerifyFailed)
	}
	if got.VerifyNote == "" || got.VerifyNote == "restored 5 tables into a throwaway database" {
		t.Errorf("manifest verify_note = %q, want the failure reason", got.VerifyNote)
	}

	// The whole point: what `status` and `metrics` read must now say failed too.
	entries, err := r.List(context.Background(), "app", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if mark := entries[0].Manifest.VerifyMark(); mark != "FAILED" {
		t.Errorf("list shows %q, want FAILED", mark)
	}
}

// A repeated passing verification must move verified_at forward, or a monitor
// asking "when did we last prove this restores?" slowly goes stale while the
// verifications are in fact still running and passing.
func TestVerifyExistingRefreshesVerifiedAt(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Now().UTC().Truncate(time.Second)
	manKey := seedUnreadable(t, dir, stamp)

	cfg := testConfig(dir, config.Retention{})
	cfg.Encryption = config.Encryption{Enabled: true, Passphrase: "wrong-passphrase"}
	r, err := NewRunner(cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}

	earlier := stamp.Add(-48 * time.Hour)
	read := func() catalog.Manifest {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, manKey))
		if err != nil {
			t.Fatal(err)
		}
		var m catalog.Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	write := func(m catalog.Manifest) {
		t.Helper()
		payload, _ := json.MarshalIndent(m, "", "  ")
		writeFile(t, filepath.Join(dir, manKey), payload)
	}

	m := read()
	m.VerifiedAt = &earlier
	write(m)

	entries, err := r.listManifests(context.Background(), r.dests[0], "app")
	if err != nil {
		t.Fatal(err)
	}
	r.recordVerifyResult(context.Background(), r.dests[0], entries[0], catalog.VerifyPassed, "restored 3 tables into a throwaway database")

	got := read()
	if got.Verify != catalog.VerifyPassed {
		t.Fatalf("verify = %q, want %q", got.Verify, catalog.VerifyPassed)
	}
	if got.VerifiedAt == nil || !got.VerifiedAt.After(earlier) {
		t.Errorf("verified_at = %v, want a time after %v", got.VerifiedAt, earlier)
	}

	// And an identical repeat still moves it forward.
	previous := *got.VerifiedAt
	entries, err = r.listManifests(context.Background(), r.dests[0], "app")
	if err != nil {
		t.Fatal(err)
	}
	r.recordVerifyResult(context.Background(), r.dests[0], entries[0], catalog.VerifyPassed, "restored 3 tables into a throwaway database")
	if again := read(); again.VerifiedAt == nil || again.VerifiedAt.Before(previous) {
		t.Errorf("verified_at went backwards: %v then %v", previous, again.VerifiedAt)
	}
}
