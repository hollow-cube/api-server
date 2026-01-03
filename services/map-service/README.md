# map-service


## DB Schema Evolution
We use [golang-migrate/migrate](https://github.com/golang-migrate/migrate) to handle postgres schema upgrades. You should
first ensure the tool is installed:

```shell
go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

To create a new migration, run the following command (**make sure to replace <migration_name> with a reasonable name**):

```shell
migrate create -ext sql -dir internal/db/migrations -seq <migration_name>
```

Before writing any migrations, make sure to read through the best practices in the 
[migrate documentation](https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md). In
particular, migrations should always be idempotent, transactional and reversible. It is almost never
valid to edit an existing migration after it has been deployed.

DB changes must always be (at least) single version backwards compatible to allow for rolling updates.

> **Note on using Tilt:**
> 
> If you are using Tilt then map-service will autorestart when you run `migrate` commands, which will
> result in the migration being automatically applied. You should disable the tilt resource while editing
> then reenable it after the migration is written.
