# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.0]: https://github.com/shaxzodbek-uzb/pgproof/releases/tag/v0.1.0
