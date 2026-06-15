package main

import (
	"context"
	"embed"
	"log"

	dbpostgres "github.com/DesKaOne/deskaone-sdk/database/postgres"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	ctx := context.Background()

	pool, err := dbpostgres.Open(ctx, dbpostgres.Config{
		Host:               "192.168.1.5",
		Port:               5432,
		User:               "deskaone",
		Password:           "change_me",
		Database:           "wolf",
		SSLMode:            "disable",
		AutoCreateDatabase: true,
		AutoMigrate:        true,
	}, migrations, "migrations")
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	log.Println("database ready")
}
