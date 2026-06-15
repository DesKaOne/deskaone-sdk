# deskaone-sdk

A small Go SDK with network/proxy/http/websocket helpers and a dialect-aware database package.

## Specification

Cross-language behavior is documented in [SDK_SPEC.md](SDK_SPEC.md).

## Database

The `database` package supports SQLite and PostgreSQL through a shared `database.Open(database.Config{...})` API. The engine is instance-based, so one process can open multiple independent database connections.

### SQLite quick start

SQLite is a good fit for local CLIs, embedded workers, local caches, and single-process apps that need a durable file-backed store without running a database server.

```go
db, err := database.Open(database.Config{
    Driver: database.DriverSQLite,
    DSN:    "data/app.db",
    AutoMigrate: true,
    Migrations: []string{
        `CREATE TABLE IF NOT EXISTS test_items (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL
        );`,
    },
})
if err != nil {
    return err
}
defer db.Close()
```

A runnable SQLite example is available at `cmd/database_sqlite_example`:

```bash
go run ./cmd/database_sqlite_example
```

### PostgreSQL quick start

PostgreSQL is a good fit for production servers, multi-worker deployments, and shared state that must be accessed safely by multiple processes or services.

```go
db, err := database.Open(database.Config{
    Driver: database.DriverPostgres,
    DSN:    "postgres://postgres:postgres@localhost:5432/deskaone?sslmode=disable",
    AutoMigrate: true,
    Migrations: []string{
        `CREATE TABLE IF NOT EXISTS test_items (
            id BIGSERIAL PRIMARY KEY,
            name TEXT NOT NULL
        );`,
    },
})
if err != nil {
    return err
}
defer db.Close()
```

The Postgres example reads its DSN from `DESKAONE_POSTGRES_DSN`:

```bash
DESKAONE_POSTGRES_DSN='postgres://postgres:postgres@localhost:5432/deskaone?sslmode=disable' \
  go run ./cmd/database_postgres_example
```

### PostgreSQL pgx auto-create and embedded migrations

For reusable SDK-style applications, `database/postgres` opens a `pgxpool.Pool`, can create the target database, and applies SQL files embedded with `embed.FS`.

```go
package main

import (
    "context"
    "embed"
    "log"

    dbpostgres "github.com/DesKaOne/deskaone-sdk/database/postgres"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
    pool, err := dbpostgres.Open(context.Background(), dbpostgres.Config{
        Host: "192.168.1.5",
        Port: 5432,
        User: "deskaone",
        Password: "change_me",
        Database: "wolf",
        SSLMode: "disable",
        AutoCreateDatabase: true,
        AutoMigrate: true,
    }, migrations, "migrations")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()
}
```

`AutoCreateDatabase=true` connects to `AdminDatabase` (default `postgres`) and requires a PostgreSQL role with `CREATEDB` permission. For production deployments, prefer `AutoCreateDatabase=false` with `AutoMigrate=true` after the database has been provisioned.

Migration files should be named with ascending numeric prefixes, such as `001_init.sql` and `002_add_indexes.sql`. Applied migrations are stored in `schema_migrations` with checksums; a checksum mismatch returns an error instead of silently rerunning changed SQL.

A runnable pgx example with embedded migrations is available at `examples/postgres_auto_migrate`:

```bash
go run ./examples/postgres_auto_migrate
```

### Migration example

Set `AutoMigrate` to `true` to run migrations during `Open`, or call `Migrate` later with an explicit context:

```go
err := db.Migrate(ctx, []string{
    `CREATE TABLE IF NOT EXISTS jobs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL
    );`,
    `CREATE INDEX IF NOT EXISTS idx_jobs_name ON jobs (name);`,
})
```

Migrations run in order inside a transaction when the driver supports transactions. The first failing migration returns an error that includes the migration index.

### Connection pool settings

Use the optional pool fields when the default `database/sql` pool behavior is not enough:

```go
db, err := database.Open(database.Config{
    Driver:          database.DriverPostgres,
    DSN:             dsn,
    MaxOpenConns:    25,
    MaxIdleConns:    10,
    ConnMaxLifetime: time.Hour,
})
```
