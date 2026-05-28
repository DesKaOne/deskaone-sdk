package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/DesKaOne/deskaone-sdk/database"
)

func main() {
	dsn := os.Getenv("DESKAONE_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("DESKAONE_POSTGRES_DSN is not set")
	}

	db, err := database.Open(database.Config{
		Driver:      database.DriverPostgres,
		DSN:         dsn,
		AutoMigrate: true,
		Migrations: []string{
			`CREATE TABLE IF NOT EXISTS test_items (
				id BIGSERIAL PRIMARY KEY,
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

	if _, err := db.DB().Exec(`INSERT INTO test_items (name) VALUES ($1)`, "postgres item"); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Postgres database ready")
}
