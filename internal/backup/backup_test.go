package backup

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig(dir string, ret config.Retention) *config.Config {
	return &config.Config{
		Databases: []config.Database{{
			Name: "app", Driver: config.DriverPostgres, Host: "h", Port: 5432,
			User: "u", Password: "p", DBName: "app", DumpFormat: config.FormatCustom,
		}},
		Destinations:   []config.Destination{{Type: config.TypeLocal, Name: "disk", Path: dir}},
		Retention:      ret,
		TimeoutSeconds: 60,
		StagingDir:     dir + "/staging",
	}
}

// seed writes an artifact + manifest pair into the local destination directory.
func seed(t *testing.T, dir string, stamp time.Time) {
	t.Helper()
	k := catalog.Keyer{}
	s := catalog.Stamp(stamp)
	artKey := k.ArtifactKey("app", s, ".dump", false)
	manKey := k.ManifestKey("app", s)

	writeFile(t, filepath.Join(dir, artKey), []byte("fake-dump-bytes"))

	man := catalog.Manifest{
		Tool: "pgproof", Database: "app", DBName: "app", Driver: config.DriverPostgres,
		Format: config.FormatCustom, Encrypted: false, Artifact: artKey,
		SizeBytes: 15, CreatedAt: stamp, Verify: catalog.VerifyPassed,
	}
	payload, _ := json.MarshalIndent(man, "", "  ")
	writeFile(t, filepath.Join(dir, manKey), payload)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListSorted(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	seed(t, dir, now.AddDate(0, 0, -2))
	seed(t, dir, now)
	seed(t, dir, now.AddDate(0, 0, -1))

	r, err := NewRunner(testConfig(dir, config.Retention{}), quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := r.List(context.Background(), "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// newest first
	if !entries[0].Stamp.After(entries[1].Stamp) || !entries[1].Stamp.After(entries[2].Stamp) {
		t.Errorf("entries not sorted newest-first: %v", entries)
	}
	if entries[0].Manifest.Verify != catalog.VerifyPassed {
		t.Errorf("manifest not loaded: %+v", entries[0].Manifest)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	stamps := []time.Time{now, now.AddDate(0, 0, -1), now.AddDate(0, 0, -2)}
	for _, s := range stamps {
		seed(t, dir, s)
	}

	r, err := NewRunner(testConfig(dir, config.Retention{KeepLast: 1}), quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Dry-run removes nothing on disk.
	dry, err := r.Prune(ctx, nil, true)
	if err != nil {
		t.Fatalf("prune dry: %v", err)
	}
	if len(dry) != 1 || dry[0].Kept != 1 || len(dry[0].Removed) != 2 {
		t.Fatalf("dry run = %+v", dry)
	}
	if got := countFiles(t, dir); got != 6 { // 3 artifacts + 3 manifests
		t.Fatalf("dry run deleted files: %d on disk, want 6", got)
	}

	// Real prune deletes the two oldest (artifact + manifest each).
	if _, err := r.Prune(ctx, nil, false); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if got := countFiles(t, dir); got != 2 {
		t.Fatalf("after prune: %d files, want 2", got)
	}

	entries, _ := r.List(ctx, "", "")
	if len(entries) != 1 {
		t.Fatalf("after prune list = %d, want 1", len(entries))
	}
}

func TestPruneWithoutPolicyErrors(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRunner(testConfig(dir, config.Retention{}), quietLogger())
	if _, err := r.Prune(context.Background(), nil, false); err == nil {
		t.Error("expected error pruning with no retention policy")
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		1024:       "1.0 KiB",
		1536:       "1.5 KiB",
		1048576:    "1.0 MiB",
		1073741824: "1.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
