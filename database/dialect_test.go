package database

import "testing"

func TestSQLitePlaceholders(t *testing.T) {
	d := SQLiteDialect{}
	if got := d.Placeholder(1); got != "?" {
		t.Fatalf("Placeholder() = %q, want ?", got)
	}
	if got := d.Placeholders(3, 1); got != "?, ?, ?" {
		t.Fatalf("Placeholders() = %q", got)
	}
}

func TestPostgresPlaceholders(t *testing.T) {
	d := PostgresDialect{}
	if got := d.Placeholders(3, 1); got != "$1, $2, $3" {
		t.Fatalf("Placeholders() = %q", got)
	}
	if got := d.Placeholders(2, 3); got != "$3, $4" {
		t.Fatalf("Placeholders() with offset = %q", got)
	}
}

func TestWhereKeyUsesAnd(t *testing.T) {
	if got := whereKey([]string{"id", "name"}); got != `"id" = ? AND "name" = ?` {
		t.Fatalf("whereKey() = %q", got)
	}
	if got := whereKeyFor(PostgresDialect{}, []string{"id", "name"}, 1); got != `"id" = $1 AND "name" = $2` {
		t.Fatalf("whereKeyFor(postgres) = %q", got)
	}
}

func TestInsertIgnoreSQL(t *testing.T) {
	cols := []string{"id", "name"}
	if got := (SQLiteDialect{}).InsertIgnore("test_items", cols, []string{"id"}); got != `INSERT OR IGNORE INTO "test_items" ("id", "name") VALUES (?, ?)` {
		t.Fatalf("sqlite InsertIgnore() = %q", got)
	}
	if got := (PostgresDialect{}).InsertIgnore("test_items", cols, []string{"id"}); got != `INSERT INTO "test_items" ("id", "name") VALUES ($1, $2) ON CONFLICT ("id") DO NOTHING` {
		t.Fatalf("postgres InsertIgnore() = %q", got)
	}
}

func TestUpsertSQL(t *testing.T) {
	cols := []string{"id", "name"}
	conflicts := []string{"id"}
	updates := []string{"name"}

	wantSQLite := `INSERT INTO "test_items" ("id", "name") VALUES (?, ?) ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name"`
	if got := (SQLiteDialect{}).Upsert("test_items", cols, conflicts, updates); got != wantSQLite {
		t.Fatalf("sqlite Upsert() = %q", got)
	}

	wantPostgres := `INSERT INTO "test_items" ("id", "name") VALUES ($1, $2) ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name"`
	if got := (PostgresDialect{}).Upsert("test_items", cols, conflicts, updates); got != wantPostgres {
		t.Fatalf("postgres Upsert() = %q", got)
	}
}
