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
func (b *BaseEngine[E, K]) Table() string                { panic("Table()") }
func (b *BaseEngine[E, K]) KeyColumns() []string         { panic("KeyColumns()") }
func (b *BaseEngine[E, K]) IntPrimaryKey() *string       { return nil }
func (b *BaseEngine[E, K]) ConflictColumns() []string    { return nil }
func (b *BaseEngine[E, K]) FromRow(*sql.Rows) (E, error) { panic("FromRow") }
func (b *BaseEngine[E, K]) KeyArgs(K) []any              { panic("KeyArgs") }

// ================= WRITE =================

func (b *BaseEngine[E, K]) Save(entity E, mode InserType) error {
	b.engine.notifyWrite()

	cols := strings.Join(b.spec.Columns(), ", ")
	vals := b.spec.Values(entity)

	q := fmt.Sprintf(
		"INSERT OR %s INTO %s (%s) VALUES (%s)",
		mode,
		b.spec.Table(),
		cols,
		placeholders(len(vals)),
	)

	_, err := b.db.Exec(q, vals...)
	return err
}

func (b *BaseEngine[E, K]) SaveBatch(
	entities []E,
	mode InserType,
) error {
	if len(entities) == 0 {
		return nil
	}

	b.engine.notifyWrite()

	cols := strings.Join(b.spec.Columns(), ", ")
	q := fmt.Sprintf(
		"INSERT OR %s INTO %s (%s) VALUES (%s)",
		mode,
		b.spec.Table(),
		cols,
		placeholders(len(b.spec.Columns())),
	)

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(q)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range entities {
		if _, err := stmt.Exec(b.spec.Values(e)...); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (b *BaseEngine[E, K]) Upsert(
	entity E,
	updateColumns []string,
) error {

	conflicts := b.spec.ConflictColumns()
	if len(conflicts) == 0 {
		return errors.New("conflictColumns not defined")
	}

	b.engine.notifyWrite()

	cols := strings.Join(b.spec.Columns(), ", ")
	vals := b.spec.Values(entity)

	updates := make([]string, len(updateColumns))
	for i, c := range updateColumns {
		updates[i] = fmt.Sprintf("%s = excluded.%s", c, c)
	}

	q := fmt.Sprintf(`
INSERT INTO %s (%s)
VALUES (%s)
ON CONFLICT(%s)
DO UPDATE SET %s
`,
		b.spec.Table(),
		cols,
		placeholders(len(vals)),
		strings.Join(conflicts, ", "),
		strings.Join(updates, ", "),
	)

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

	conflicts := b.spec.ConflictColumns()
	if len(conflicts) == 0 {
		return errors.New("conflictColumns not defined")
	}

	b.engine.notifyWrite()

	cols := strings.Join(b.spec.Columns(), ", ")

	updates := make([]string, len(updateColumns))
	for i, c := range updateColumns {
		updates[i] = fmt.Sprintf("%s = excluded.%s", c, c)
	}

	q := fmt.Sprintf(`
INSERT INTO %s (%s)
VALUES (%s)
ON CONFLICT(%s)
DO UPDATE SET %s
`,
		b.spec.Table(),
		cols,
		placeholders(len(b.spec.Columns())),
		strings.Join(conflicts, ", "),
		strings.Join(updates, ", "),
	)

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(q)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range entities {
		if _, err := stmt.Exec(b.spec.Values(e)...); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// ================= READ =================

func (b *BaseEngine[E, K]) Find(key K) (*E, error) {
	where := whereKey(b.spec.KeyColumns())
	q := fmt.Sprintf(
		"SELECT * FROM %s WHERE %s LIMIT 1",
		b.spec.Table(),
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
	return nil, nil
}

func (b *BaseEngine[E, K]) Load(
	mode PaginationMode,
	limit int,
	offset int,
	lastID *int,
) ([]E, error) {

	switch mode {

	case PaginationOffset:
		rows, err := b.db.Query(
			fmt.Sprintf("SELECT * FROM %s LIMIT ? OFFSET ?", b.spec.Table()),
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
		return out, nil

	case PaginationKeyset:
		pk := b.spec.IntPrimaryKey()
		if pk == nil || lastID == nil {
			return nil, errors.New("keyset pagination not supported")
		}

		q := fmt.Sprintf(`
SELECT * FROM %s
WHERE %s > ?
ORDER BY %s
LIMIT ?
`,
			b.spec.Table(),
			*pk,
			*pk,
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
		return out, nil
	}

	return nil, errors.New("invalid pagination mode")
}

// ================= DELETE =================

func (b *BaseEngine[E, K]) Delete(key K) error {
	b.engine.notifyWrite()

	where := whereKey(b.spec.KeyColumns())
	q := fmt.Sprintf("DELETE FROM %s WHERE %s", b.spec.Table(), where)

	_, err := b.db.Exec(q, b.spec.KeyArgs(key)...)
	return err
}

func (b *BaseEngine[E, K]) DeleteAll() error {
	b.engine.notifyWrite()
	_, err := b.db.Exec("DELETE FROM " + b.spec.Table())
	return err
}

func (b *BaseEngine[E, K]) Count() (int, error) {
	var c int
	err := b.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", b.spec.Table())).Scan(&c)
	return c, err
}

func (b *BaseEngine[E, K]) IsEmptyFast() (bool, error) {
	row := b.db.QueryRow(
		fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", b.spec.Table()),
	)
	var x int
	err := row.Scan(&x)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return false, err
}

/* ================= helpers ================= */

func whereKey(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c + " = ?"
	}
	return strings.Join(out, " AND ")
}

func placeholders(n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = "?"
	}
	return strings.Join(out, ", ")
}
