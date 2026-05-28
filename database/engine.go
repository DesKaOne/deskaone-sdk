package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

import _ "modernc.org/sqlite"

type DatabaseEngine struct {
	db      *sql.DB
	driver  Driver
	dialect Dialect
	sqls    []string

	lastWrite time.Time
	mu        sync.Mutex

	scheduler *DBMaintenanceScheduler
}

func Open(config Config) (*DatabaseEngine, error) {
	if config.Driver == "" {
		return nil, ErrEmptyDriver
	}
	if strings.TrimSpace(config.DSN) == "" {
		return nil, ErrEmptyDSN
	}

	dialect, err := dialectForDriver(config.Driver)
	if err != nil {
		return nil, err
	}

	if config.Driver == DriverSQLite {
		if err := ensureSQLiteParentDir(config.DSN); err != nil {
			return nil, err
		}
	}

	if !isSQLDriverRegistered(dialect.DriverName()) {
		return nil, fmt.Errorf("database driver %q is not registered", dialect.DriverName())
	}

	db, err := sql.Open(dialect.DriverName(), config.DSN)
	if err != nil {
		return nil, err
	}
	if config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
	}

	engine := &DatabaseEngine{
		db:      db,
		driver:  config.Driver,
		dialect: dialect,
		sqls:    config.Migrations,
	}

	if err := dialect.Init(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if config.AutoMigrate {
		if err := engine.Migrate(context.Background(), config.Migrations); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	if config.EnableMaintenance && dialect.SupportsMaintenance() {
		engine.scheduler = NewDBMaintenanceScheduler(engine)
		engine.scheduler.Start()
	}

	return engine, nil
}

func isSQLDriverRegistered(name string) bool {
	for _, registered := range sql.Drivers() {
		if registered == name {
			return true
		}
	}
	return false
}

func Start(fileName string, sqlFiles []string, table []string) (*DatabaseEngine, error) {
	dir := "database"
	path := filepath.Join(dir, fileName)
	return startWithPath(path, sqlFiles, table)
}

func StartAtPath(path string, sqlFiles []string, table []string) (*DatabaseEngine, error) {
	return startWithPath(path, sqlFiles, table)
}

func startWithPath(path string, sqlFiles []string, table []string) (*DatabaseEngine, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path is empty")
	}

	var migrations []string
	var err error
	if len(sqlFiles) != 0 {
		migrations, err = loadSQLFiles(sqlFiles)
		if err != nil {
			return nil, err
		}
	} else if len(table) != 0 {
		migrations = table
	} else {
		return nil, fmt.Errorf("no database migrations provided")
	}

	return Open(Config{
		Driver:            DriverSQLite,
		DSN:               path,
		AutoMigrate:       true,
		Migrations:        migrations,
		EnableMaintenance: true,
	})
}

func ensureSQLiteParentDir(dsn string) error {
	if dsn == ":memory:" || strings.HasPrefix(dsn, "file:") {
		return nil
	}
	parentDir := filepath.Dir(dsn)
	if parentDir == "." || parentDir == "" {
		return nil
	}
	return os.MkdirAll(parentDir, 0755)
}

func (e *DatabaseEngine) DB() *sql.DB { return e.db }

func (e *DatabaseEngine) Driver() Driver { return e.driver }

func (e *DatabaseEngine) Ping(ctx context.Context) error { return e.db.PingContext(ctx) }

func (e *DatabaseEngine) Close() error {
	if e.scheduler != nil {
		e.scheduler.Stop()
	}
	return e.db.Close()
}

func loadSQLFiles(paths []string) ([]string, error) {
	var out []string
	var buf strings.Builder

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}

		data, readErr := io.ReadAll(f)
		closeErr := f.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			buf.WriteString(line)
			buf.WriteByte(' ')
			if strings.HasSuffix(line, ";") {
				out = append(out, strings.TrimSpace(strings.TrimSuffix(buf.String(), ";")))
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		out = append(out, strings.TrimSpace(buf.String()))
	}
	return out, nil
}

func (e *DatabaseEngine) notifyWrite() {
	e.mu.Lock()
	e.lastWrite = time.Now()
	e.mu.Unlock()
}

func (e *DatabaseEngine) isWriteHot(cooldown time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lastWrite.IsZero() {
		return false
	}
	return time.Since(e.lastWrite) < cooldown
}

func (e *DatabaseEngine) checkpoint(truncate bool) error {
	if !e.dialect.SupportsMaintenance() {
		return nil
	}
	if truncate {
		return e.exec(`PRAGMA wal_checkpoint(TRUNCATE);`)
	}
	return e.exec(`PRAGMA wal_checkpoint(PASSIVE);`)
}

func (e *DatabaseEngine) maybeVacuum(threshold float64) error {
	if !e.dialect.SupportsMaintenance() {
		return nil
	}
	var pageCount, free int
	if err := e.db.QueryRow(`PRAGMA page_count;`).Scan(&pageCount); err != nil {
		return err
	}
	if err := e.db.QueryRow(`PRAGMA freelist_count;`).Scan(&free); err != nil {
		return err
	}

	if pageCount == 0 {
		return nil
	}

	ratio := float64(free) / float64(pageCount)
	if ratio >= threshold {
		return e.vacuum()
	}
	return nil
}

func (e *DatabaseEngine) vacuum() error {
	start := time.Now()
	fmt.Println("[DB] VACUUM start")
	if err := e.exec(`PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
		return err
	}
	if err := e.exec(`VACUUM;`); err != nil {
		return err
	}
	fmt.Println("[DB] VACUUM done in", time.Since(start))
	return nil
}

func (e *DatabaseEngine) exec(q string) error {
	_, err := e.db.Exec(q)
	return err
}
