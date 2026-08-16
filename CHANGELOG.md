# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **`go install` builds reported their version as `dev`.** The release ldflags
  only apply to the GoReleaser build, so anyone installing with
  `go install github.com/shaxzodbek-uzb/pgproof@v0.2.0` — the usual way to
  install a Go tool — got a binary that could not say which version it was. That
  is a poor answer from a backup tool being quoted in an incident report. The
  version, commit and date now fall back to the module and VCS metadata the Go
  toolchain embeds on its own. A stamped value still wins, since a release build
  knows its tag exactly.
- The banner now says `commit dated` rather than `built` when the timestamp came
  from VCS metadata, because that is the commit time — a `go install` build has
  no build timestamp to report.

## [0.2.0] - 2026-08-16

### Added
- `pgproof status` — per-database backup health (last backup, age, size, verify
  status, and the last backup that actually *passed* a restore test). Exits `2`
  when any database is unhealthy, so it works directly as a monitoring check.
  `--json` for the structured report, `--max-age` for a staleness threshold.
- `pgproof metrics` — the same picture in the Prometheus text exposition format.
  `-o FILE` writes atomically for the node_exporter textfile collector.
- `pgproof run --metrics-addr` — serve `/metrics` from the long-lived scheduler.
- Webhook notifications (`notify.webhook`): a JSON POST to any endpoint, with
  optional headers. Slack and Discord incoming webhooks work as-is.

## [0.1.0] - 2026-06-20

### Added
- Initial release of pgproof.
- `backup`, `verify`, `restore`, `list`, `prune`, `run`, `test`, `init`,
  `keygen` commands.
- Logical backups for Postgres (`pg_dump`, custom/plain) and MySQL (`mysqldump`).
- **Verified restores**: each backup is restored into a throwaway database and
  sanity-checked, with optional end-to-end verification of the stored artifact
  (`verify.from_remote`).
- Streaming [age](https://age-encryption.org) encryption (passphrase or
  public-key recipients).
- Destinations: S3-compatible (AWS S3, Cloudflare R2, DigitalOcean Spaces,
  MinIO), local filesystem, and Telegram (write-only off-site copy).
- Telegram and healthchecks.io notifications.
- Grandfather-father-son retention (`keep last/daily/weekly/monthly`).
- Built-in cron scheduler (`pgproof run`).

[Unreleased]: https://github.com/shaxzodbek-uzb/pgproof/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/shaxzodbek-uzb/pgproof/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/shaxzodbek-uzb/pgproof/releases/tag/v0.1.0
