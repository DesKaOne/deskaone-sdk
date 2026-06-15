// Package postgres contains reusable PostgreSQL helpers built on pgx/v5.
//
// It can create a database when needed, open a pgxpool.Pool, and apply SQL
// migrations embedded with Go embed.FS. AutoCreateDatabase requires a role with
// CREATEDB permission. In production, prefer provisioning databases separately
// and running with AutoCreateDatabase false and AutoMigrate true.
package postgres
