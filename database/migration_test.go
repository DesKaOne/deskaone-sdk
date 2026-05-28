package database

import (
	"context"
	"testing"
)

func TestOpenSQLiteAndMigrate(t *testing.T) {
	db, err := Open(Config{
		Driver:      DriverSQLite,
		DSN:         ":memory:",
		AutoMigrate: true,
		Migrations: []string{
			`CREATE TABLE test_items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if db.Driver() != DriverSQLite {
		t.Fatalf("Driver() = %q", db.Driver())
	}
	if err := db.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`INSERT INTO test_items (name) VALUES (?)`, "item"); err != nil {
		t.Fatal(err)
	}
}
