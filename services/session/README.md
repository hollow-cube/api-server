# Session Service

The session service is responsible for keeping track of all players currently online (network wide in the future, but
for now just mapmaker).

## Session Tracking
The session service needs to keep a record (in redis for now) of every online player, and remove them when they
disconnect.

The Java server (proxy in the future) handles this by sending requests to the create and delete session endpoints:
- `POST /session/{playerId}` (on join)
- `DELETE /session/{playerId}` (on leave)

This works fine in the happy case, but we need to handle a case where the Minecraft server crashes unexpectedly,
it would be problematic if the session service still thought the player was online. This is handled by the session
service server tracking, see below.

## Server Tracking
The session service needs to keep a record (in redis for now) of every running backend server to be able to route
players to the correct server, as well as know when a server goes down (incl. unexpected crashes). To accomplish this,
the session service uses the Kubernetes client API to watch for changes to the server state. There is a single instance
of the session service selected at any given moment to track the server state. This is done using the Kubernetes client
leadership election API.

The leader instance of the session service will watch for changes and update the server state in redis. Any service
instance (including the leader) may query the server state when assigning players.

## Chat
Chat is handled by the session service also. Chat is done over Kafka. TODO write more

## Local Development
You must have the following installed:
- goimports: `go install golang.org/x/tools/cmd/goimports@latest`
- [openapi-go](https://github.com/mworzala/openapi-go): `go install github.com/mworzala/openapi-go@latest`

You should use our Tilt setup to run the service locally, see [here](https://github.com/hollow-cube/development).

## DB Schema Evolution
We use [golang-migrate/migrate](https://github.com/golang-migrate/migrate) to handle postgres schema upgrades.

To create a new migration, run the following command (**make sure to replace <migration_name> with a reasonable name**):

```shell
go tool migrate create -ext sql -dir internal/pkg/storage/migrate -seq <migration_name>
```

Before writing any migrations, make sure to read through the best practices in the
[migrate documentation](https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md). In
particular, migrations should always be idempotent, transactional and reversible. It is almost never
valid to edit an existing migration after it has been deployed.

DB changes must always be (at least) single version backwards compatible to allow for rolling updates.

> **Note on using Tilt:**
>
> If you are using Tilt then the service will autorestart when you run `migrate` commands, which will
> result in the migration being automatically applied. You should disable the tilt resource while editing
> then reenable it after the migration is written.
