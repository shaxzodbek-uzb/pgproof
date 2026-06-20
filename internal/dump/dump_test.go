package dump

import (
	"testing"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

func TestArtifactExt(t *testing.T) {
	cases := []struct {
		driver, format, want string
	}{
		{config.DriverPostgres, config.FormatCustom, ".dump"},
		{config.DriverPostgres, config.FormatPlain, ".sql.gz"},
		{config.DriverMySQL, config.FormatPlain, ".sql.gz"},
	}
	for _, c := range cases {
		got := ArtifactExt(config.Database{Driver: c.driver, DumpFormat: c.format})
		if got != c.want {
			t.Errorf("ArtifactExt(%s,%s) = %q, want %q", c.driver, c.format, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"17.0", "16.3", 1},
		{"16.3", "17.0", -1},
		{"16.3", "16.3", 0},
		{"16.10", "16.9", 1},
		{"15", "", 1},
		{"9.6.24", "10.0", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestEscapePgpass(t *testing.T) {
	got := escapePgpass(`p:a\s`)
	if got != `p\:a\\s` {
		t.Errorf("escapePgpass = %q", got)
	}
}
