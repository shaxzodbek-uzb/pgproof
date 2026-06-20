// Package config loads, expands and validates the pgproof YAML configuration.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level pgproof configuration.
type Config struct {
	Databases    []Database    `yaml:"databases"`
	Encryption   Encryption    `yaml:"encryption"`
	Destinations []Destination `yaml:"destinations"`
	Retention    Retention     `yaml:"retention"`
	Verify       Verify        `yaml:"verify"`
	Notify       Notify        `yaml:"notify"`
	Schedule     Schedule      `yaml:"schedule"`

	StagingDir     string `yaml:"staging_dir"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	LogLevel       string `yaml:"log_level"`
	LogFormat      string `yaml:"log_format"`
}

// Database describes a single database to back up.
type Database struct {
	Name       string `yaml:"name"`
	Driver     string `yaml:"driver"` // postgres | mysql
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	User       string `yaml:"user"`
	Password   string `yaml:"password"`
	DBName     string `yaml:"dbname"`
	SSLMode    string `yaml:"sslmode"`     // postgres: disable|require|verify-full ...
	DumpPath   string `yaml:"dump_path"`   // optional dir holding pg_dump/mysqldump
	DumpFormat string `yaml:"dump_format"` // postgres: custom|plain (mysql is always plain)
	Jobs       int    `yaml:"jobs"`        // parallel jobs for custom-format dump/restore
	// AdminDSN is the maintenance connection used to create/drop the throwaway
	// database during verify (e.g. the "postgres" database on the same server).
	// When empty, verify derives it from the connection above using the
	// driver's default maintenance database.
	AdminDB string `yaml:"admin_db"`
}

// Encryption controls at-rest encryption of dump artifacts (age format).
type Encryption struct {
	Enabled    bool     `yaml:"enabled"`
	Passphrase string   `yaml:"passphrase"`
	Recipients []string `yaml:"recipients"` // age X25519 public recipients
	Identity   string   `yaml:"identity"`   // age secret key (for decrypt/restore/verify)
}

// Destination is a place to store backups.
type Destination struct {
	Type string `yaml:"type"` // s3 | local | telegram
	Name string `yaml:"name"`

	// s3 / S3-compatible (AWS, Cloudflare R2, DigitalOcean Spaces, MinIO)
	Bucket    string `yaml:"bucket"`
	Prefix    string `yaml:"prefix"`
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	PathStyle bool   `yaml:"path_style"`

	// local
	Path string `yaml:"path"`

	// telegram (write-only off-site copy / alerts)
	BotToken  string `yaml:"bot_token"`
	ChatID    string `yaml:"chat_id"`
	MaxSizeMB int    `yaml:"max_size_mb"`
}

// Retention is the prune policy applied per destination.
type Retention struct {
	KeepLast    int `yaml:"keep_last"`
	KeepDaily   int `yaml:"keep_daily"`
	KeepWeekly  int `yaml:"keep_weekly"`
	KeepMonthly int `yaml:"keep_monthly"`
}

// Any reports whether at least one retention rule is set.
func (r Retention) Any() bool {
	return r.KeepLast > 0 || r.KeepDaily > 0 || r.KeepWeekly > 0 || r.KeepMonthly > 0
}

// Verify configures the restore-test that proves a backup is recoverable.
type Verify struct {
	Enabled bool `yaml:"enabled"`
	// FromRemote verifies the uploaded copy (download → decrypt → restore),
	// proving the stored artifact end-to-end. When false, the local staged
	// dump is verified (faster, still proves dump integrity + restorability).
	FromRemote     bool     `yaml:"from_remote"`
	MinTables      int      `yaml:"min_tables"`
	RowCountTables []string `yaml:"row_count_tables"`
}

// Notify configures success/failure notifications.
type Notify struct {
	Telegram     TelegramNotify     `yaml:"telegram"`
	Healthchecks HealthchecksNotify `yaml:"healthchecks"`
}

// TelegramNotify sends run summaries to a Telegram chat.
type TelegramNotify struct {
	Enabled   bool   `yaml:"enabled"`
	BotToken  string `yaml:"bot_token"`
	ChatID    string `yaml:"chat_id"`
	OnSuccess bool   `yaml:"on_success"`
	OnFailure bool   `yaml:"on_failure"`
}

// HealthchecksNotify pings a healthchecks.io-style dead-man's-switch URL.
type HealthchecksNotify struct {
	Enabled bool   `yaml:"enabled"`
	PingURL string `yaml:"ping_url"`
}

// Schedule drives the built-in `pgproof run` scheduler.
type Schedule struct {
	Cron     string `yaml:"cron"`
	Timezone string `yaml:"timezone"`
	Prune    bool   `yaml:"prune"` // run retention after each scheduled backup
}

// Load reads, env-expands and validates the config at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes config from raw YAML bytes (after ${ENV} expansion).
func Parse(raw []byte) (*Config, error) {
	expanded, err := expandEnv(string(raw))
	if err != nil {
		return nil, err
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// expandEnv replaces ${VAR} and ${VAR:-default} with environment values.
// Bare $VAR is intentionally NOT expanded so a literal '$' in passwords
// survives. An unset ${VAR} with no default is a hard error — but only when it
// appears in an actual value; ${VAR} written inside a YAML comment is ignored so
// documentation does not trip the check.
func expandEnv(s string) (string, error) {
	if missing := missingVars(stripYAMLComments(s)); len(missing) > 0 {
		return "", fmt.Errorf("config references unset environment variables: %s", strings.Join(missing, ", "))
	}
	out := envRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRe.FindStringSubmatch(m)
		name, hasDefault, def := sub[1], sub[2] != "", sub[3]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		if hasDefault {
			return def
		}
		return m // unset & in a comment — leave the literal untouched
	})
	return out, nil
}

// missingVars returns the de-duplicated names of unset variables that have no
// default in the given (comment-stripped) text.
func missingVars(s string) []string {
	var missing []string
	for _, m := range envRe.FindAllStringSubmatch(s, -1) {
		name, hasDefault := m[1], m[2] != ""
		if hasDefault {
			continue
		}
		if _, ok := os.LookupEnv(name); !ok {
			missing = append(missing, name)
		}
	}
	return dedupe(missing)
}

// stripYAMLComments blanks out the comment portion of each line so ${VAR}
// references in comments are not treated as required. It respects quoted
// scalars and YAML's rule that '#' only starts a comment at line start or after
// whitespace.
func stripYAMLComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = stripLineComment(line)
	}
	return strings.Join(lines, "\n")
}

func stripLineComment(line string) string {
	var inSingle, inDouble bool
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return line[:i]
			}
		}
	}
	return line
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func (c *Config) applyDefaults() {
	if c.StagingDir == "" {
		c.StagingDir = defaultStagingDir()
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 3600
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.LogFormat == "" {
		c.LogFormat = "text"
	}
	if c.Schedule.Timezone == "" {
		c.Schedule.Timezone = "UTC"
	}
	if c.Verify.MinTables == 0 {
		c.Verify.MinTables = 1
	}
	for i := range c.Databases {
		db := &c.Databases[i]
		if db.Driver == "" {
			db.Driver = DriverPostgres
		}
		if db.Port == 0 {
			db.Port = defaultPort(db.Driver)
		}
		if db.DBName == "" {
			db.DBName = db.Name
		}
		if db.DumpFormat == "" {
			db.DumpFormat = FormatCustom
		}
		if db.Driver == DriverMySQL {
			db.DumpFormat = FormatPlain // mysqldump has no custom format
		}
		if db.Jobs == 0 {
			db.Jobs = 1
		}
	}
	for i := range c.Destinations {
		d := &c.Destinations[i]
		if d.Type == TypeTelegram && d.MaxSizeMB == 0 {
			d.MaxSizeMB = 50 // Telegram bot sendDocument limit
		}
		if d.Type == TypeS3 && d.Region == "" {
			d.Region = "us-east-1"
		}
	}
}

func defaultPort(driver string) int {
	if driver == DriverMySQL {
		return 3306
	}
	return 5432
}

func defaultStagingDir() string {
	if d := os.Getenv("PGPROOF_STAGING_DIR"); d != "" {
		return d
	}
	return os.TempDir() + string(os.PathSeparator) + "pgproof"
}

// Driver and destination type constants.
const (
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"

	FormatCustom = "custom"
	FormatPlain  = "plain"

	TypeS3       = "s3"
	TypeLocal    = "local"
	TypeTelegram = "telegram"
)
