CREATE TABLE IF NOT EXISTS sessions (
  id                           INTEGER PRIMARY KEY,
  started_at                   TIMESTAMP NOT NULL,
  ended_at                     TIMESTAMP,
  exam_type                    TEXT NOT NULL,
  exam_id                      TEXT,
  mode                         TEXT NOT NULL,
  order_strategy               TEXT NOT NULL,
  time_limit_seconds           INTEGER,
  question_time_limit_seconds  INTEGER,
  exit_reason                  TEXT,
  planned_questions            TEXT,
  topic                        TEXT,
  part                         TEXT,
  question_limit               INTEGER,
  question_number              INTEGER
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

-- question_srs holds one Leitner-box scheduling row per question that has
-- been answered at least once. A question with no row here has never been
-- attempted, so it's neither "due" nor "not due" - it's simply new.
CREATE TABLE IF NOT EXISTS question_srs (
  question_id      TEXT PRIMARY KEY,
  box              INTEGER NOT NULL,
  due_at           TIMESTAMP NOT NULL,
  last_reviewed_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_question_srs_due_at ON question_srs(due_at);
