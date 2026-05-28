package database

import (
	"database/sql"
	"fmt"
)

type PostgresDialect struct{}

func (PostgresDialect) DriverName() string { return "postgres" }

func (PostgresDialect) Placeholder(index int) string { return fmt.Sprintf("$%d", index) }

func (d PostgresDialect) Placeholders(count int, startIndex int) string {
	return placeholdersFor(d, count, startIndex)
}

func (PostgresDialect) QuoteIdent(name string) string { return quoteIdent(name) }

func (d PostgresDialect) Insert(table string, cols []string) string {
	return insertSQL(d, "INSERT", table, cols)
}

func (d PostgresDialect) InsertIgnore(table string, cols []string, conflictCols []string) string {
	target := conflictTarget(d, conflictCols)
	if target == "" {
		return fmt.Sprintf("%s ON CONFLICT DO NOTHING", d.Insert(table, cols))
	}
	return fmt.Sprintf("%s ON CONFLICT %s DO NOTHING", d.Insert(table, cols), target)
}

func (d PostgresDialect) InsertReplace(table string, cols []string, conflictCols []string) string {
	return d.Upsert(table, cols, conflictCols, cols)
}

func (d PostgresDialect) Upsert(table string, cols []string, conflictCols []string, updateCols []string) string {
	return fmt.Sprintf(
		"%s ON CONFLICT %s DO UPDATE SET %s",
		d.Insert(table, cols),
		conflictTarget(d, conflictCols),
		updateAssignments(d, updateCols),
	)
}

func (PostgresDialect) Init(*sql.DB) error { return nil }

func (PostgresDialect) SupportsMaintenance() bool { return false }
