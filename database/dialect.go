package database

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

type Dialect interface {
	DriverName() string
	Placeholder(index int) string
	Placeholders(count int, startIndex int) string

	QuoteIdent(name string) string

	Insert(table string, cols []string) string
	InsertIgnore(table string, cols []string, conflictCols []string) string
	InsertReplace(table string, cols []string, conflictCols []string) string
	Upsert(table string, cols []string, conflictCols []string, updateCols []string) string

	Init(db *sql.DB) error
	SupportsMaintenance() bool
}

func dialectForDriver(driver Driver) (Dialect, error) {
	switch driver {
	case DriverSQLite:
		return SQLiteDialect{}, nil
	case DriverPostgres:
		return PostgresDialect{}, nil
	case "":
		return nil, ErrEmptyDriver
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, driver)
	}
}

func validateIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("%w: identifier is empty", ErrInvalidIdentifier)
	}
	for _, r := range name {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return fmt.Errorf("%w: %q", ErrInvalidIdentifier, name)
	}
	return nil
}

func quoteIdent(name string) string {
	if err := validateIdentifier(name); err != nil {
		return ""
	}
	return `"` + name + `"`
}

func quoteIdents(d Dialect, names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = d.QuoteIdent(name)
	}
	return out
}

func joinedQuotedIdents(d Dialect, names []string) string {
	return strings.Join(quoteIdents(d, names), ", ")
}

func insertSQL(d Dialect, verb, table string, cols []string) string {
	return fmt.Sprintf(
		"%s INTO %s (%s) VALUES (%s)",
		verb,
		d.QuoteIdent(table),
		joinedQuotedIdents(d, cols),
		d.Placeholders(len(cols), 1),
	)
}

func conflictTarget(d Dialect, conflictCols []string) string {
	if len(conflictCols) == 0 {
		return ""
	}
	return fmt.Sprintf("(%s)", joinedQuotedIdents(d, conflictCols))
}

func updateAssignments(d Dialect, updateCols []string) string {
	out := make([]string, len(updateCols))
	for i, col := range updateCols {
		quoted := d.QuoteIdent(col)
		out[i] = fmt.Sprintf("%s = EXCLUDED.%s", quoted, quoted)
	}
	return strings.Join(out, ", ")
}
