package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/DesKaOne/deskaone-sdk/database"
)

func main() {
	dsn := filepath.Join(os.TempDir(), "deskaone_sqlite_example.db")
	db, err := database.Open(database.Config{
		Driver:      database.DriverSQLite,
		DSN:         dsn,
		AutoMigrate: true,
		Migrations: []string{
			`CREATE TABLE IF NOT EXISTS test_items (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL
			);`,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(context.Background()); err != nil {
		log.Fatal(err)
	}

	if _, err := db.DB().Exec(`INSERT INTO test_items (name) VALUES (?)`, "sqlite item"); err != nil {
		log.Fatal(err)
	}

	fmt.Println("SQLite database ready at", dsn)
}
