package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// identRe matches an optionally schema-qualified SQL identifier. row_count_tables
// entries are interpolated into a SELECT, so anything outside this is rejected up
// front rather than quoted-and-hoped.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// Validate checks the config for completeness and obvious mistakes, returning
// every problem found (joined) rather than just the first.
func (c *Config) Validate() error {
	var errs []error

	if len(c.Databases) == 0 {
		errs = append(errs, errors.New("at least one entry under `databases` is required"))
	}
	if len(c.Destinations) == 0 {
		errs = append(errs, errors.New("at least one entry under `destinations` is required"))
	}

	names := map[string]bool{}
	for i, db := range c.Databases {
		ctx := fmt.Sprintf("databases[%d]", i)
		if db.Name == "" {
			errs = append(errs, fmt.Errorf("%s: `name` is required", ctx))
		} else if names[db.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate database name %q", ctx, db.Name))
		}
		names[db.Name] = true

		if db.Driver != DriverPostgres && db.Driver != DriverMySQL {
			errs = append(errs, fmt.Errorf("%s: `driver` must be %q or %q, got %q", ctx, DriverPostgres, DriverMySQL, db.Driver))
		}
		if db.Host == "" {
			errs = append(errs, fmt.Errorf("%s: `host` is required", ctx))
		}
		if db.User == "" {
			errs = append(errs, fmt.Errorf("%s: `user` is required", ctx))
		}
		if db.DBName == "" {
			errs = append(errs, fmt.Errorf("%s: `dbname` is required", ctx))
		}
		if db.Driver == DriverPostgres && db.DumpFormat != FormatCustom && db.DumpFormat != FormatPlain {
			errs = append(errs, fmt.Errorf("%s: `dump_format` must be %q or %q", ctx, FormatCustom, FormatPlain))
		}
	}

	destNames := map[string]bool{}
	for i, d := range c.Destinations {
		ctx := fmt.Sprintf("destinations[%d]", i)
		if d.Name == "" {
			errs = append(errs, fmt.Errorf("%s: `name` is required", ctx))
		} else if destNames[d.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate destination name %q", ctx, d.Name))
		}
		destNames[d.Name] = true

		switch d.Type {
		case TypeS3:
			if d.Bucket == "" {
				errs = append(errs, fmt.Errorf("%s (s3 %q): `bucket` is required", ctx, d.Name))
			}
			if d.AccessKey == "" || d.SecretKey == "" {
				errs = append(errs, fmt.Errorf("%s (s3 %q): `access_key` and `secret_key` are required", ctx, d.Name))
			}
		case TypeLocal:
			if d.Path == "" {
				errs = append(errs, fmt.Errorf("%s (local %q): `path` is required", ctx, d.Name))
			}
		case TypeTelegram:
			if d.BotToken == "" || d.ChatID == "" {
				errs = append(errs, fmt.Errorf("%s (telegram %q): `bot_token` and `chat_id` are required", ctx, d.Name))
			}
		case "":
			errs = append(errs, fmt.Errorf("%s: `type` is required (s3|local|telegram)", ctx))
		default:
			errs = append(errs, fmt.Errorf("%s: unknown type %q (want s3|local|telegram)", ctx, d.Type))
		}
	}

	if c.Encryption.Enabled {
		if c.Encryption.Passphrase == "" && len(c.Encryption.Recipients) == 0 {
			errs = append(errs, errors.New("encryption.enabled is true but neither `passphrase` nor `recipients` is set"))
		}
		if c.Encryption.Passphrase != "" && len(c.Encryption.Recipients) > 0 {
			errs = append(errs, errors.New("encryption: set either `passphrase` or `recipients`, not both"))
		}
		// Recipient-only encryption needs an identity to decrypt for verify/restore.
		if len(c.Encryption.Recipients) > 0 && c.Encryption.Identity == "" {
			errs = append(errs, errors.New("encryption: `identity` (age secret key) is required to decrypt when using `recipients`"))
		}
	}

	if c.Verify.Enabled && c.Verify.FromRemote && !c.hasRestoreCapableDestination() {
		errs = append(errs, errors.New("verify.from_remote requires an s3 or local destination to read the artifact back from"))
	}
	for _, tbl := range c.Verify.RowCountTables {
		if !identRe.MatchString(tbl) {
			errs = append(errs, fmt.Errorf("verify.row_count_tables: %q is not a valid table name (use `table` or `schema.table`)", tbl))
		}
	}

	return joinErrors(errs)
}

// RestoreCapableDestination returns the first destination that supports reading
// artifacts back (s3 or local). Telegram is write-only.
func (c *Config) RestoreCapableDestination() (Destination, bool) {
	for _, d := range c.Destinations {
		if d.Type == TypeS3 || d.Type == TypeLocal {
			return d, true
		}
	}
	return Destination{}, false
}

func (c *Config) hasRestoreCapableDestination() bool {
	_, ok := c.RestoreCapableDestination()
	return ok
}

// DatabaseByName returns the configured database with the given name.
func (c *Config) DatabaseByName(name string) (Database, bool) {
	for _, db := range c.Databases {
		if db.Name == name {
			return db, true
		}
	}
	return Database{}, false
}

// DestinationByName returns the configured destination with the given name.
func (c *Config) DestinationByName(name string) (Destination, bool) {
	for _, d := range c.Destinations {
		if d.Name == name {
			return d, true
		}
	}
	return Destination{}, false
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("invalid configuration:")
	for _, e := range errs {
		b.WriteString("\n  - ")
		b.WriteString(e.Error())
	}
	return errors.New(b.String())
}
