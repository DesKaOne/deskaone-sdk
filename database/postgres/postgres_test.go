package postgres

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestReadMigrationsSortsByNumericPrefix(t *testing.T) {
	migrationsFS := fstest.MapFS{
		"migrations/010_ten.sql": {Data: []byte("SELECT 10;")},
		"migrations/002_two.sql": {Data: []byte("SELECT 2;")},
		"migrations/001_one.sql": {Data: []byte("SELECT 1;")},
		"migrations/readme.txt":  {Data: []byte("ignored")},
	}

	migrations, err := readMigrations(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}

	got := []int64{migrations[0].version, migrations[1].version, migrations[2].version}
	want := []int64{1, 2, 10}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("versions = %v, want %v", got, want)
		}
	}
}

func TestReadMigrationsRejectsDuplicateVersions(t *testing.T) {
	migrationsFS := fstest.MapFS{
		"migrations/001_init.sql":  {Data: []byte("SELECT 1;")},
		"migrations/001_again.sql": {Data: []byte("SELECT 2;")},
	}

	_, err := readMigrations(migrationsFS, "migrations")
	if err == nil {
		t.Fatal("expected duplicate version error")
	}
}

func TestCheckAppliedMigrationRejectsChecksumMismatch(t *testing.T) {
	m := migration{
		version:  1,
		name:     "001_init.sql",
		checksum: checksumSQL("SELECT 1;"),
	}

	err := checkAppliedMigration(m, "001_init.sql", checksumSQL("SELECT 2;"))
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestQuoteIdentEscapesDoubleQuotes(t *testing.T) {
	got := quoteIdent(`app"db`)
	want := `"app""db"`
	if got != want {
		t.Fatalf("quoteIdent() = %q, want %q", got, want)
	}
}

func TestReadMigrationsMissingDirectory(t *testing.T) {
	_, err := readMigrations(fstest.MapFS{}, "migrations")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want fs.ErrNotExist", err)
	}
}
