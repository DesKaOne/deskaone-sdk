package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type EngineSpec[E any, K any] interface {
	Table() string

	// WRITE
	Columns() []string
	Values(E) []any
	ConflictColumns() []string

	// READ
	FromRow(*sql.Rows) (E, error)

	// KEY
	KeyColumns() []string
	KeyArgs(K) []any
	IntPrimaryKey() *string
}

type InserType string

const (
	InsertReplace InserType = "REPLACE"
	InsertIgnore  InserType = "IGNORE"
	ModeUpsert    InserType = "UPSERT"
)

type PaginationMode int

const (
	PaginationOffset PaginationMode = iota
	PaginationKeyset
)

type BaseEngine[E any, K any] struct {
	engine *DatabaseEngine
	db     *sql.DB
	spec   EngineSpec[E, K]
}

func NewBaseEngine[E any, K any](
	e *DatabaseEngine,
	spec EngineSpec[E, K],
) *BaseEngine[E, K] {
	return &BaseEngine[E, K]{
		engine: e,
		db:     e.db,
		spec:   spec,
	}
}

/*
⚠️ DI-OVERRIDE OLEH ENGINE SPESIFIK
*/
func (b *BaseEngine[E, K]) Table() string             { return "" }
func (b *BaseEngine[E, K]) KeyColumns() []string      { return nil }
func (b *BaseEngine[E, K]) IntPrimaryKey() *string    { return nil }
func (b *BaseEngine[E, K]) ConflictColumns() []string { return nil }
func (b *BaseEngine[E, K]) FromRow(*sql.Rows) (E, error) {
	var zero E
	return zero, errors.New("FromRow not implemented")
}
func (b *BaseEngine[E, K]) KeyArgs(K) []any { return nil }

// ================= WRITE =================

func (b *BaseEngine[E, K]) Save(entity E, mode InserType) error {
	if err := b.validateTableAndColumns(); err != nil {
		return err
	}
	b.engine.notifyWrite()

	vals := b.spec.Values(entity)
	if len(vals) != len(b.spec.Columns()) {
		return fmt.Errorf("values count %d does not match columns count %d", len(vals), len(b.spec.Columns()))
	}

	q, err := b.insertQuery(mode, nil)
	if err != nil {
		return err
	}

	_, err = b.db.Exec(q, vals...)
	return err
}

func (b *BaseEngine[E, K]) SaveBatch(
	entities []E,
	mode InserType,
) error {
	if len(entities) == 0 {
		return nil
	}
	if err := b.validateTableAndColumns(); err != nil {
		return err
	}

	b.engine.notifyWrite()

	q, err := b.insertQuery(mode, nil)
	if err != nil {
		return err
	}

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(q)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range entities {
		vals := b.spec.Values(e)
		if len(vals) != len(b.spec.Columns()) {
			_ = tx.Rollback()
			return fmt.Errorf("values count %d does not match columns count %d", len(vals), len(b.spec.Columns()))
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (b *BaseEngine[E, K]) Upsert(
	entity E,
	updateColumns []string,
) error {
	if err := b.validateTableAndColumns(updateColumns...); err != nil {
		return err
	}

	conflicts := b.spec.ConflictColumns()
	if len(conflicts) == 0 {
		return errors.New("conflictColumns not defined")
	}
	if len(updateColumns) == 0 {
		return errors.New("updateColumns not defined")
	}
	if err := validateIdentifiers(conflicts...); err != nil {
		return err
	}

	b.engine.notifyWrite()

	vals := b.spec.Values(entity)
	if len(vals) != len(b.spec.Columns()) {
		return fmt.Errorf("values count %d does not match columns count %d", len(vals), len(b.spec.Columns()))
	}

	q := b.engine.dialect.Upsert(b.spec.Table(), b.spec.Columns(), conflicts, updateColumns)
	_, err := b.db.Exec(q, vals...)
	return err
}

func (b *BaseEngine[E, K]) UpsertBatch(
	entities []E,
	updateColumns []string,
) error {
	if len(entities) == 0 {
		return nil
	}
	if err := b.validateTableAndColumns(updateColumns...); err != nil {
		return err
	}

	conflicts := b.spec.ConflictColumns()
	if len(conflicts) == 0 {
		return errors.New("conflictColumns not defined")
	}
	if len(updateColumns) == 0 {
		return errors.New("updateColumns not defined")
	}
	if err := validateIdentifiers(conflicts...); err != nil {
		return err
	}

	b.engine.notifyWrite()

	q := b.engine.dialect.Upsert(b.spec.Table(), b.spec.Columns(), conflicts, updateColumns)

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(q)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range entities {
		vals := b.spec.Values(e)
		if len(vals) != len(b.spec.Columns()) {
			_ = tx.Rollback()
			return fmt.Errorf("values count %d does not match columns count %d", len(vals), len(b.spec.Columns()))
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (b *BaseEngine[E, K]) insertQuery(mode InserType, updateColumns []string) (string, error) {
	d := b.engine.dialect
	switch mode {
	case InsertIgnore:
		return d.InsertIgnore(b.spec.Table(), b.spec.Columns(), b.spec.ConflictColumns()), nil
	case InsertReplace:
		if b.engine.driver == DriverPostgres && len(b.spec.ConflictColumns()) == 0 {
			return "", errors.New("postgres replace requires conflictColumns")
		}
		return d.InsertReplace(b.spec.Table(), b.spec.Columns(), b.spec.ConflictColumns()), nil
	case ModeUpsert:
		if len(b.spec.ConflictColumns()) == 0 {
			return "", errors.New("conflictColumns not defined")
		}
		if len(updateColumns) == 0 {
			updateColumns = b.spec.Columns()
		}
		return d.Upsert(b.spec.Table(), b.spec.Columns(), b.spec.ConflictColumns(), updateColumns), nil
	default:
		return d.Insert(b.spec.Table(), b.spec.Columns()), nil
	}
}

// ================= READ =================

func (b *BaseEngine[E, K]) Find(key K) (*E, error) {
	if err := b.validateTableAndColumns(b.spec.KeyColumns()...); err != nil {
		return nil, err
	}
	where := whereKeyFor(b.engine.dialect, b.spec.KeyColumns(), 1)
	q := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s LIMIT 1",
		b.selectColumns(),
		b.engine.dialect.QuoteIdent(b.spec.Table()),
		where,
	)

	rows, err := b.db.Query(q, b.spec.KeyArgs(key)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		e, err := b.spec.FromRow(rows)
		return &e, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (b *BaseEngine[E, K]) Load(
	mode PaginationMode,
	limit int,
	offset int,
	lastID *int,
) ([]E, error) {
	if err := b.validateTableAndColumns(); err != nil {
		return nil, err
	}

	switch mode {

	case PaginationOffset:
		rows, err := b.db.Query(
			fmt.Sprintf(
				"SELECT %s FROM %s LIMIT %s OFFSET %s",
				b.selectColumns(),
				b.engine.dialect.QuoteIdent(b.spec.Table()),
				b.engine.dialect.Placeholder(1),
				b.engine.dialect.Placeholder(2),
			),
			limit, offset,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var out []E
		for rows.Next() {
			e, err := b.spec.FromRow(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, rows.Err()

	case PaginationKeyset:
		pk := b.spec.IntPrimaryKey()
		if pk == nil || lastID == nil {
			return nil, errors.New("keyset pagination not supported")
		}
		if err := validateIdentifier(*pk); err != nil {
			return nil, err
		}

		q := fmt.Sprintf(`
SELECT %s FROM %s
WHERE %s > %s
ORDER BY %s
LIMIT %s
`,
			b.selectColumns(),
			b.engine.dialect.QuoteIdent(b.spec.Table()),
			b.engine.dialect.QuoteIdent(*pk),
			b.engine.dialect.Placeholder(1),
			b.engine.dialect.QuoteIdent(*pk),
			b.engine.dialect.Placeholder(2),
		)

		rows, err := b.db.Query(q, *lastID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var out []E
		for rows.Next() {
			e, err := b.spec.FromRow(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, rows.Err()
	}

	return nil, errors.New("invalid pagination mode")
}

// ================= DELETE =================

func (b *BaseEngine[E, K]) Delete(key K) error {
	if err := b.validateTableAndColumns(b.spec.KeyColumns()...); err != nil {
		return err
	}
	b.engine.notifyWrite()

	where := whereKeyFor(b.engine.dialect, b.spec.KeyColumns(), 1)
	q := fmt.Sprintf("DELETE FROM %s WHERE %s", b.engine.dialect.QuoteIdent(b.spec.Table()), where)

	_, err := b.db.Exec(q, b.spec.KeyArgs(key)...)
	return err
}

func (b *BaseEngine[E, K]) DeleteAll() error {
	if err := b.validateTableAndColumns(); err != nil {
		return err
	}
	b.engine.notifyWrite()
	_, err := b.db.Exec("DELETE FROM " + b.engine.dialect.QuoteIdent(b.spec.Table()))
	return err
}

func (b *BaseEngine[E, K]) Count() (int, error) {
	if err := b.validateTableAndColumns(); err != nil {
		return 0, err
	}
	var c int
	err := b.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", b.engine.dialect.QuoteIdent(b.spec.Table()))).Scan(&c)
	return c, err
}

func (b *BaseEngine[E, K]) IsEmptyFast() (bool, error) {
	if err := b.validateTableAndColumns(); err != nil {
		return false, err
	}
	row := b.db.QueryRow(
		fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", b.engine.dialect.QuoteIdent(b.spec.Table())),
	)
	var x int
	err := row.Scan(&x)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return false, err
}

func (b *BaseEngine[E, K]) selectColumns() string {
	cols := b.spec.Columns()
	if len(cols) == 0 {
		return "*"
	}
	return joinedQuotedIdents(b.engine.dialect, cols)
}

func (b *BaseEngine[E, K]) validateTableAndColumns(extraColumns ...string) error {
	if err := validateIdentifier(b.spec.Table()); err != nil {
		return err
	}
	return validateIdentifiers(append(append([]string{}, b.spec.Columns()...), extraColumns...)...)
}

func validateIdentifiers(names ...string) error {
	for _, name := range names {
		if err := validateIdentifier(name); err != nil {
			return err
		}
	}
	return nil
}

/* ================= helpers ================= */

func whereKey(cols []string) string {
	return whereKeyFor(SQLiteDialect{}, cols, 1)
}

func whereKeyFor(d Dialect, cols []string, startIndex int) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = fmt.Sprintf("%s = %s", d.QuoteIdent(c), d.Placeholder(startIndex+i))
	}
	return strings.Join(out, " AND ")
}

func placeholders(n int) string {
	return placeholdersFor(SQLiteDialect{}, n, 1)
}

func placeholdersFor(d Dialect, n int, startIndex int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = d.Placeholder(startIndex + i)
	}
	return strings.Join(out, ", ")
}
