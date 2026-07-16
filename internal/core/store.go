package core

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  id                           INTEGER PRIMARY KEY,
  started_at                   TIMESTAMP NOT NULL,
  ended_at                     TIMESTAMP,
  exam_type                    TEXT NOT NULL,
  exam_id                      TEXT,
  mode                         TEXT NOT NULL,
  order_strategy                TEXT NOT NULL,
  time_limit_seconds            INTEGER,
  question_time_limit_seconds  INTEGER,
  exit_reason                  TEXT
);

CREATE TABLE IF NOT EXISTS attempts (
  id             INTEGER PRIMARY KEY,
  session_id     INTEGER NOT NULL REFERENCES sessions(id),
  question_id    TEXT NOT NULL,
  answer         TEXT NOT NULL,
  correct        BOOLEAN NOT NULL,
  timed_out      BOOLEAN NOT NULL DEFAULT FALSE,
  time_taken_ms  INTEGER,
  answered_at    TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_attempts_question ON attempts(question_id);
CREATE INDEX IF NOT EXISTS idx_attempts_session ON attempts(session_id);
CREATE INDEX IF NOT EXISTS idx_attempts_answered_at ON attempts(answered_at);
`

// DefaultDBPath resolves the XDG-appropriate progress database path.
func DefaultDBPath() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "itpec-trainer", "progress.db"), nil
}

// OpenStore opens (creating if absent) the progress database at path, in WAL mode,
// and ensures the schema exists.
func OpenStore(ctx context.Context, path string) (*sql.DB, error) {
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
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return db, nil
}
