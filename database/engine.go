package database

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // 🔥 WAJIB
)

type DatabaseEngine struct {
	db   *sql.DB
	sqls []string

	lastWrite time.Time
	mu        sync.Mutex

	scheduler *DBMaintenanceScheduler
}

var instance *DatabaseEngine
var once sync.Once

func Start(fileName string, sqlFiles []string, table []string) (*DatabaseEngine, error) {
	dir := "database"
	path := filepath.Join(dir, fileName)
	return startWithPath(path, sqlFiles, table)
}

func StartAtPath(path string, sqlFiles []string, table []string) (*DatabaseEngine, error) {
	return startWithPath(path, sqlFiles, table)
}

func startWithPath(path string, sqlFiles []string, table []string) (*DatabaseEngine, error) {
	var err error

	once.Do(func() {
		path = strings.TrimSpace(path)
		if path == "" {
			err = fmt.Errorf("database path is empty")
			return
		}

		parentDir := filepath.Dir(path)
		if parentDir != "." && parentDir != "" {
			if mkErr := os.MkdirAll(parentDir, 0755); mkErr != nil {
				err = mkErr
				return
			}
		}

		db, e := sql.Open("sqlite", path)
		if e != nil {
			err = e
			return
		}
		var sqls []string
		if len(sqlFiles) != 0 {
			sqls, e = loadSQLFiles(sqlFiles)
			if e != nil {
				err = e
				return
			}
		} else if len(table) != 0 {
			sqls = table
		} else {
			err = fmt.Errorf("")
			return
		}

		engine := &DatabaseEngine{
			db:   db,
			sqls: sqls,
		}
		engine.init()
		engine.scheduler = NewDBMaintenanceScheduler(engine)
		engine.scheduler.Start()

		instance = engine
	})

	return instance, err
}

func (e *DatabaseEngine) DB() *sql.DB {
	return e.db
}

func (e *DatabaseEngine) init() {
	e.exec(`PRAGMA journal_mode = WAL;`)
	e.exec(`PRAGMA synchronous = NORMAL;`)
	e.exec(`PRAGMA wal_autocheckpoint = 1000;`)

	for _, stmt := range e.sqls {
		if _, err := e.db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate") {
				continue
			}
			panic(fmt.Errorf("SQL INIT ERROR: %w\n%s", err, stmt))
		}
	}
}

func loadSQLFiles(paths []string) ([]string, error) {
	var out []string
	var buf strings.Builder

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		data, _ := io.ReadAll(f)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			buf.WriteString(line)
			buf.WriteByte(' ')
			if strings.HasSuffix(line, ";") {
				out = append(out, strings.TrimSuffix(buf.String(), ";"))
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
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

func (e *DatabaseEngine) checkpoint(truncate bool) {
	if truncate {
		e.exec(`PRAGMA wal_checkpoint(TRUNCATE);`)
	} else {
		e.exec(`PRAGMA wal_checkpoint(PASSIVE);`)
	}
}

func (e *DatabaseEngine) maybeVacuum(threshold float64) {
	var pageCount, free int
	e.db.QueryRow(`PRAGMA page_count;`).Scan(&pageCount)
	e.db.QueryRow(`PRAGMA freelist_count;`).Scan(&free)

	if pageCount == 0 {
		return
	}

	ratio := float64(free) / float64(pageCount)
	if ratio >= threshold {
		e.vacuum()
	}
}

func (e *DatabaseEngine) vacuum() {
	fmt.Println("[DB] VACUUM start")
	start := time.Now()

	e.exec(`PRAGMA wal_checkpoint(TRUNCATE);`)
	e.exec(`VACUUM;`)

	fmt.Println("[DB] VACUUM done in", time.Since(start))
}

func (e *DatabaseEngine) exec(q string) {
	if _, err := e.db.Exec(q); err != nil {
		panic(err)
	}
}

func (e *DatabaseEngine) Close() {
	if e.scheduler != nil {
		e.scheduler.Stop()
	}
	_ = e.db.Close()
	instance = nil
}
