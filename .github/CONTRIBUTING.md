# Contributing

Thanks for your interest in contributing! This document covers the basics you need to know before opening a pull
request.

## Getting Started

See [Development Setup](../.github/DEVELOPMENT_SETUP.md) for instructions on building and running the project locally.

## Before You Contribute

All contributors must sign our Contributor License Agreement.
You'll be automatically prompted by CLA Assistant when you open your first pull request.

## Pull Requests

- Please open and issue or discuss on Discord before opening a PR (even for bug fixes).
  This helps ensure that your contribution is aligned with our goals and avoids duplicate/wasted effort.
- Keep PRs relatively focused on a single change. Smaller PRs are easier to review and more likely to be merged quickly.
- Follow go formatting conventions. Please avoid PRs that only change formatting or style.
- We don't have a set review period, please be patient while we review.

## DB Schema Evolution

We use [golang-migrate/migrate](https://github.com/golang-migrate/migrate) for postgres schema migrations.

To create a new migration (**replace `<migration_name>` with a descriptive name**, and choose a directory):

```shell
go tool migrate create -ext sql -dir internal/(db|playerdb|mapdb)/storage/migrate -seq <migration_name>
```

Read through the [migrate best practices](https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md) before
writing migrations. Migrations must be idempotent, transactional, and backwards compatible. Never edit a migration that
has already been deployed. We do not use down migrations currently.

DB changes must always be at least one version backwards compatible to support rolling updates.

> **Note on Tilt:** The service auto-restarts when you run `migrate` commands, which applies the migration
> automatically. Disable the Tilt resource while writing a migration, then re-enable it once done.

## Communication

For questions, discussion, or if you're unsure whether a change would be welcome, please ask in the`#general-dev`
channel in our [Discord](https://discord.hollowcube.net).
