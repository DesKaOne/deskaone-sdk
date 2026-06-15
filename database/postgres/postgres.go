// Package postgres provides pgx-based PostgreSQL helpers for opening a pool,
// creating a database, and applying embedded SQL migrations.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockKey int64 = 729469221415001

type Config struct {
	Host          string
	Port          int
	User          string
	Password      string
	Database      string
	AdminDatabase string
	SSLMode       string

	AutoCreateDatabase bool
	AutoMigrate        bool

	MaxConns int32
	MinConns int32
}

type migration struct {
	version  int64
	name     string
	path     string
	sql      string
	checksum string
}

// Open connects to PostgreSQL, optionally creates the database, and optionally
// applies embedded SQL migrations before returning a ready-to-use pool.
func Open(ctx context.Context, cfg Config, migrations fs.FS, migrationsDir string) (*pgxpool.Pool, error) {
	cfg = normalizeConfig(cfg)
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	if cfg.Database == "" {
		return nil, errors.New("postgres: database is required")
	}

	if cfg.AutoCreateDatabase {
		if err := EnsureDatabase(ctx, cfg); err != nil {
			return nil, err
		}
	}

	poolConfig, err := pgxpool.ParseConfig(connString(cfg, cfg.Database))
	if err != nil {
		return nil, fmt.Errorf("postgres: parse pool config: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolConfig.MinConns = cfg.MinConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect to database %q: %w", cfg.Database, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping database %q: %w", cfg.Database, err)
	}

	if cfg.AutoMigrate {
		if err := Migrate(ctx, pool, migrations, migrationsDir); err != nil {
			pool.Close()
			return nil, err
		}
	}

	return pool, nil
}

// EnsureDatabase creates cfg.Database from cfg.AdminDatabase when it does not exist.
func EnsureDatabase(ctx context.Context, cfg Config) error {
	cfg = normalizeConfig(cfg)
	if cfg.Database == "" {
		return errors.New("postgres: database is required")
	}

	conn, err := pgx.Connect(ctx, connString(cfg, cfg.AdminDatabase))
	if err != nil {
		return fmt.Errorf("postgres: connect to admin database %q: %w", cfg.AdminDatabase, err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, cfg.Database).Scan(&exists); err != nil {
		return fmt.Errorf("postgres: check database %q: %w", cfg.Database, err)
	}
	if exists {
		return nil
	}

	if _, err := conn.Exec(ctx, `CREATE DATABASE `+quoteIdent(cfg.Database)); err != nil {
		return fmt.Errorf("postgres: create database %q: %w", cfg.Database, err)
	}
	return nil
}

// Migrate creates and maintains schema_migrations, then applies embedded SQL
// migration files in numeric-prefix order.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsFS fs.FS, migrationsDir string) error {
	if pool == nil {
		return errors.New("postgres: pool is nil")
	}
	if migrationsFS == nil {
		return errors.New("postgres: migrations filesystem is nil")
	}
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		name TEXT NOT NULL,
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);`); err != nil {
		return fmt.Errorf("postgres: create schema_migrations table: %w", err)
	}

	if _, err := pool.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("postgres: acquire migration advisory lock: %w", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	parsed, err := readMigrations(migrationsFS, migrationsDir)
	if err != nil {
		return err
	}

	for _, m := range parsed {
		var appliedName, appliedChecksum string
		err := pool.QueryRow(ctx, `SELECT name, checksum FROM schema_migrations WHERE version = $1`, m.version).Scan(&appliedName, &appliedChecksum)
		if err == nil {
			if err := checkAppliedMigration(m, appliedName, appliedChecksum); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: check migration %d: %w", m.version, err)
		}

		if err := applyMigration(ctx, pool, m); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, m migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin migration %s: %w", m.name, err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return fmt.Errorf("postgres: execute migration %s: %w", m.name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`, m.version, m.name, m.checksum); err != nil {
		return fmt.Errorf("postgres: record migration %s: %w", m.name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit migration %s: %w", m.name, err)
	}
	return nil
}

func checkAppliedMigration(m migration, appliedName, appliedChecksum string) error {
	if appliedChecksum != m.checksum {
		return fmt.Errorf("postgres: migration checksum mismatch for version %d (%s): applied checksum %s differs from embedded checksum %s", m.version, appliedName, appliedChecksum, m.checksum)
	}
	return nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 5432
	}
	if cfg.AdminDatabase == "" {
		cfg.AdminDatabase = "postgres"
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}
	return cfg
}

func connString(cfg Config, database string) string {
	u := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     "/" + database,
		RawQuery: url.Values{"sslmode": []string{cfg.SSLMode}}.Encode(),
	}
	if cfg.User != "" {
		if cfg.Password != "" {
			u.User = url.UserPassword(cfg.User, cfg.Password)
		} else {
			u.User = url.User(cfg.User)
		}
	}
	return u.String()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func checksumSQL(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

func readMigrations(migrationsFS fs.FS, migrationsDir string) ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("postgres: migrations directory %q does not exist: %w", migrationsDir, err)
		}
		return nil, fmt.Errorf("postgres: read migrations directory %q: %w", migrationsDir, err)
	}

	seen := make(map[int64]string)
	out := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, err := parseMigrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		if existing, ok := seen[version]; ok {
			return nil, fmt.Errorf("postgres: duplicate migration version %d in %q and %q", version, existing, entry.Name())
		}
		seen[version] = entry.Name()

		migrationPath := path.Join(migrationsDir, entry.Name())
		content, err := fs.ReadFile(migrationsFS, migrationPath)
		if err != nil {
			return nil, fmt.Errorf("postgres: read migration %q: %w", migrationPath, err)
		}
		sql := string(content)
		out = append(out, migration{
			version:  version,
			name:     entry.Name(),
			path:     migrationPath,
			sql:      sql,
			checksum: checksumSQL(sql),
		})
	}

	sortMigrations(out)
	return out, nil
}

func parseMigrationVersion(name string) (int64, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok || prefix == "" {
		return 0, fmt.Errorf("postgres: migration %q must start with a numeric prefix followed by underscore", name)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("postgres: migration %q has invalid numeric prefix", name)
	}
	return version, nil
}

func sortMigrations(migrations []migration) {
	for i := 1; i < len(migrations); i++ {
		current := migrations[i]
		j := i - 1
		for j >= 0 && migrations[j].version > current.version {
			migrations[j+1] = migrations[j]
			j--
		}
		migrations[j+1] = current
	}
}
