package database

import "time"

type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverPostgres Driver = "postgres"
)

type Config struct {
	Driver Driver
	DSN    string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration

	AutoMigrate bool
	Migrations  []string

	EnableMaintenance bool
}
