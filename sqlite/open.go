package sqlite

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"database/sql"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Open opens (creating if absent) the progress database at path, in WAL mode,
// ensures the schema exists, and brings an already-existing database up to
// date via migrate. schema.sql is the single source of truth for the
// sessions/attempts DDL — sqlc also reads it (see sqlc.yaml) to generate the
// types and queries in this package, so it must stay valid to run verbatim
// here (CREATE TABLE/INDEX IF NOT EXISTS) as well as to sqlc; schema changes
// to an existing database instead go through migrations (see migrate.go).
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for db: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	sessionsTableExisted, err := tableExists(ctx, db, "sessions")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("check existing schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(ctx, db, sessionsTableExisted); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
