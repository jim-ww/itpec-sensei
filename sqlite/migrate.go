package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations is every schema change ever made to an existing database,
// applied in order after brand-new-database creation. schema.sql is always
// the current full shape (what a fresh database gets via CREATE TABLE IF NOT
// EXISTS) — an entry here exists only to bring an already-existing database
// up to match it. Append, never edit or remove past entries.
var migrations = []string{
	// 1: sessions.tags — comma-joined tag filter, added alongside topic/part.
	`ALTER TABLE sessions ADD COLUMN tags TEXT;`,
	// 2: sessions.unanswered — restrict pool to never-attempted questions.
	`ALTER TABLE sessions ADD COLUMN unanswered BOOLEAN NOT NULL DEFAULT FALSE;`,
}

// migrate brings db up to the latest schema. Tracked via SQLite's built-in
// PRAGMA user_version (no extra table needed) — but that pragma defaults to
// 0 on both a brand-new database and a pre-migrations database that's never
// had it set, so the two cases are told apart by whether the sessions table
// already existed before schema.sql ran: a brand-new database is created
// already in the latest shape (schema.sql), so it's stamped straight to
// len(migrations) without running any of them; an existing one runs
// whichever migrations are still pending.
func migrate(ctx context.Context, db *sql.DB, sessionsTableExisted bool) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version;`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if !sessionsTableExisted {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d;`, len(migrations))); err != nil {
			return fmt.Errorf("stamp new database as up to date: %w", err)
		}
		return nil
	}

	for i := version; i < len(migrations); i++ {
		if _, err := db.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("apply migration %d: %w", i+1, err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d;`, i+1)); err != nil {
			return fmt.Errorf("record migration %d: %w", i+1, err)
		}
	}
	return nil
}

// tableExists reports whether name is a table in db.
func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var dummy int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?;`, name).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
