# Contributing to pgproof

Thanks for helping! pgproof aims to be a small, sharp, dependable tool — PRs that
keep it that way are very welcome.

## Development

```sh
git clone https://github.com/shaxzodbek-uzb/pgproof
cd pgproof
make build      # builds ./pgproof
make test       # go test ./...
make check      # gofmt + go vet + tests
```

You need Go 1.23+. The unit tests do **not** require a running database — the
shell-out paths (pg_dump/pg_restore/psql/mysql) are isolated behind small
packages so the rest can be tested with fakes and a local destination.

To exercise a real end-to-end backup+verify, point a `pgproof.yml` at a local
Postgres and run `pgproof backup`.

## Guidelines

- Keep the dependency list tiny. New direct dependencies need a good reason.
- Credentials must never reach `argv`. Use `PGPASSFILE` / `--defaults-extra-file`.
- Anything that streams (dumps, encryption, uploads) must not buffer whole
  dumps in memory.
- Run `make check` before pushing; CI runs the same.
- One focused change per PR. Add a test for behaviour changes.

## Reporting bugs

Open an issue with your OS, `pgproof --version`, the (redacted) config, and the
command + output. For security issues, see [SECURITY.md](SECURITY.md) instead.
