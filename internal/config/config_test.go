package config

import (
	"strings"
	"testing"
)

const minimal = `
databases:
  - name: app
    driver: postgres
    host: 127.0.0.1
    user: postgres
    password: secret
    dbname: app
destinations:
  - type: local
    name: disk
    path: /tmp/x
`

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	db := cfg.Databases[0]
	if db.Port != 5432 {
		t.Errorf("default port = %d, want 5432", db.Port)
	}
	if db.DumpFormat != FormatCustom {
		t.Errorf("default format = %q, want custom", db.DumpFormat)
	}
	if cfg.TimeoutSeconds != 3600 {
		t.Errorf("default timeout = %d", cfg.TimeoutSeconds)
	}
	if cfg.StagingDir == "" {
		t.Error("staging dir should default")
	}
}

func TestEnvExpansion(t *testing.T) {
	t.Setenv("PW", "s3cr3t")
	src := strings.Replace(minimal, "password: secret", "password: ${PW}", 1)
	cfg, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Databases[0].Password != "s3cr3t" {
		t.Errorf("password = %q, want s3cr3t", cfg.Databases[0].Password)
	}
}

func TestEnvDefaultSyntax(t *testing.T) {
	src := strings.Replace(minimal, "password: secret", "password: ${MISSING:-fallback}", 1)
	cfg, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Databases[0].Password != "fallback" {
		t.Errorf("password = %q, want fallback", cfg.Databases[0].Password)
	}
}

func TestUnsetEnvInValueErrors(t *testing.T) {
	src := strings.Replace(minimal, "password: secret", "password: ${DEFINITELY_UNSET_VAR}", 1)
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("expected error for unset env var in value")
	}
}

func TestUnsetEnvInCommentIgnored(t *testing.T) {
	src := "# docs mention ${SOME_UNSET_DOC_VAR} and ${OTHER:-x}\n" + minimal
	if _, err := Parse([]byte(src)); err != nil {
		t.Fatalf("comment env var should be ignored, got: %v", err)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"unknown dest type": `
databases: [{name: a, driver: postgres, host: h, user: u, password: p, dbname: a}]
destinations: [{type: ftp, name: x}]
`,
		"missing bucket": `
databases: [{name: a, driver: postgres, host: h, user: u, password: p, dbname: a}]
destinations: [{type: s3, name: x, access_key: k, secret_key: s}]
`,
		"encryption without secret": `
databases: [{name: a, driver: postgres, host: h, user: u, password: p, dbname: a}]
destinations: [{type: local, name: x, path: /t}]
encryption: {enabled: true}
`,
		"duplicate db names": `
databases:
  - {name: a, driver: postgres, host: h, user: u, password: p, dbname: a}
  - {name: a, driver: postgres, host: h, user: u, password: p, dbname: b}
destinations: [{type: local, name: x, path: /t}]
`,
		"no databases": `
destinations: [{type: local, name: x, path: /t}]
`,
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestRestoreCapableDestination(t *testing.T) {
	cfg, err := Parse([]byte(`
databases: [{name: a, driver: postgres, host: h, user: u, password: p, dbname: a}]
destinations:
  - {type: telegram, name: tg, bot_token: t, chat_id: c}
  - {type: s3, name: r2, bucket: b, access_key: k, secret_key: s}
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, ok := cfg.RestoreCapableDestination()
	if !ok || d.Name != "r2" {
		t.Errorf("RestoreCapableDestination = %v, %v; want r2", d.Name, ok)
	}
}

func TestRowCountTablesValidation(t *testing.T) {
	good := minimal + "\nverify:\n  enabled: true\n  row_count_tables: [users, public.accounts]\n"
	if _, err := Parse([]byte(good)); err != nil {
		t.Fatalf("valid row_count_tables rejected: %v", err)
	}
	bad := minimal + "\nverify:\n  enabled: true\n  row_count_tables: [\"users; DROP TABLE x; --\"]\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error for injection-y row_count_tables entry")
	}
}

func TestKnownFieldsRejectsTypos(t *testing.T) {
	src := strings.Replace(minimal, "timeout_seconds", "", 1)
	src += "\ntimoeut_seconds: 10\n"
	if _, err := Parse([]byte(src)); err == nil {
		t.Error("expected unknown-field error for typo'd key")
	}
}
