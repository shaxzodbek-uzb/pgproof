# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities.

Instead, use GitHub's private vulnerability reporting
("Report a vulnerability" under the repository's **Security** tab), or email the
maintainer at the address on their GitHub profile. You'll get an
acknowledgement within a few days and credit in the fix unless you prefer to
stay anonymous.

## Scope & design notes

pgproof handles database credentials and your backups, so a few properties
matter:

- **Credentials never appear on `argv`.** Postgres credentials go through a
  temporary `PGPASSFILE` (mode `0600`); MySQL through a temporary
  `--defaults-extra-file` (mode `0600`). Neither is visible in `ps`.
- **Encryption is streaming age.** Plaintext dumps exist only in a `0700`
  staging directory and are deleted after each run.
- **The config can hold secrets.** Prefer `${ENV}` interpolation over inline
  values and keep `pgproof.yml` mode `0600`.
- **Verify creates and drops a throwaway database** on your server using the
  configured admin connection. It always attempts the `DROP`, even on failure.

## Supported versions

The latest released `0.x` minor receives security fixes. pgproof is pre-1.0;
pin a version and watch releases.
