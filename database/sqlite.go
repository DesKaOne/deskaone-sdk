package database

import (
	"database/sql"
	"fmt"
)

type SQLiteDialect struct{}

func (SQLiteDialect) DriverName() string { return "sqlite" }

func (SQLiteDialect) Placeholder(int) string { return "?" }

func (d SQLiteDialect) Placeholders(count int, startIndex int) string {
	return placeholdersFor(d, count, startIndex)
}

func (SQLiteDialect) QuoteIdent(name string) string { return quoteIdent(name) }

func (d SQLiteDialect) Insert(table string, cols []string) string {
	return insertSQL(d, "INSERT", table, cols)
}

func (d SQLiteDialect) InsertIgnore(table string, cols []string, _ []string) string {
	return insertSQL(d, "INSERT OR IGNORE", table, cols)
}

func (d SQLiteDialect) InsertReplace(table string, cols []string, _ []string) string {
	return insertSQL(d, "INSERT OR REPLACE", table, cols)
}

func (d SQLiteDialect) Upsert(table string, cols []string, conflictCols []string, updateCols []string) string {
	return fmt.Sprintf(
		"%s ON CONFLICT %s DO UPDATE SET %s",
		d.Insert(table, cols),
		conflictTarget(d, conflictCols),
		updateAssignments(d, updateCols),
	)
}

func (SQLiteDialect) Init(db *sql.DB) error {
	for _, stmt := range []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
		`PRAGMA foreign_keys = ON;`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (SQLiteDialect) SupportsMaintenance() bool { return true }
