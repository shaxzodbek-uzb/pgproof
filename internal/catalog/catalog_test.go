package catalog

import (
	"testing"
	"time"
)

func TestKeyer(t *testing.T) {
	k := Keyer{Prefix: "pgproof"}
	stamp := "20260620T030000Z"

	art := k.ArtifactKey("app", stamp, ".dump", true)
	if art != "pgproof/app/app-20260620T030000Z.dump.age" {
		t.Errorf("artifact key = %q", art)
	}
	plain := k.ArtifactKey("app", stamp, ".sql.gz", false)
	if plain != "pgproof/app/app-20260620T030000Z.sql.gz" {
		t.Errorf("plain artifact key = %q", plain)
	}
	man := k.ManifestKey("app", stamp)
	if man != "pgproof/app/app-20260620T030000Z.json" {
		t.Errorf("manifest key = %q", man)
	}
	if pfx := k.DBPrefix("app"); pfx != "pgproof/app/" {
		t.Errorf("db prefix = %q", pfx)
	}
}

func TestKeyerEmptyPrefix(t *testing.T) {
	k := Keyer{}
	if got := k.ArtifactKey("db", "20260620T030000Z", ".dump", false); got != "db/db-20260620T030000Z.dump" {
		t.Errorf("artifact key = %q", got)
	}
	if got := k.DBPrefix("db"); got != "db/" {
		t.Errorf("db prefix = %q", got)
	}
}

func TestStampRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 20, 3, 4, 5, 0, time.UTC)
	s := Stamp(now)
	got, ok := StampOf("pgproof/app/app-" + s + ".dump.age")
	if !ok {
		t.Fatal("StampOf failed to parse")
	}
	if !got.Equal(now) {
		t.Errorf("round trip = %v, want %v", got, now)
	}
}

func TestIsManifest(t *testing.T) {
	if !IsManifest("a/b-20260620T030000Z.json") {
		t.Error("expected manifest")
	}
	if IsManifest("a/b-20260620T030000Z.dump") {
		t.Error("dump is not a manifest")
	}
}

func TestVerifyMark(t *testing.T) {
	if (Manifest{Verify: VerifyPassed}).VerifyMark() != "verified" {
		t.Error("passed mark")
	}
	if (Manifest{Verify: VerifyFailed}).VerifyMark() != "FAILED" {
		t.Error("failed mark")
	}
	if (Manifest{Verify: VerifySkipped}).VerifyMark() != "unverified" {
		t.Error("skipped mark")
	}
}
