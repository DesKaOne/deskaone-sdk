# deskaone-sdk

A small Go SDK with network/proxy/http/websocket helpers and a dialect-aware database package.

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
