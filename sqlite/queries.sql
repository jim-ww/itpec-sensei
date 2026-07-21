-- Attempts --
--
-- These return unordered rows; sorting by newest/oldest and applying a limit
-- both happen in Go (repo.go), not here - this app runs against a single
-- local user's data (at most a few thousand attempts), so fetch-then-sort-in-
-- Go is simpler than juggling per-direction SQL variants, with no meaningful
-- cost.
--
-- NOTE: keep this file ASCII-only. sqlc's sqlite-engine slice() rewriter has
-- a byte-offset bug: a multi-byte UTF-8 character (e.g. an em-dash) anywhere
-- earlier in the file shifts byte offsets and silently corrupts the
-- generated SQL for every later sqlc.slice() query. Also keep each
-- sqlc.slice() query on a single line with no comment directly above it -
-- both also corrupt the rewrite.

-- name: InsertAttempt :exec
INSERT INTO attempts (session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: DeleteAttempt :exec
DELETE FROM attempts WHERE id = ?;

-- name: GetLastAttemptForSession :one
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at
FROM attempts WHERE session_id = ? ORDER BY answered_at DESC, id DESC LIMIT 1;

-- name: GetLastAttemptAny :one
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at
FROM attempts ORDER BY answered_at DESC, id DESC LIMIT 1;

-- name: ListAttemptsBySession :many
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at
FROM attempts WHERE session_id = ?;

-- name: ListAllAttempts :many
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at
FROM attempts;

-- name: ListAllAttemptsSince :many
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at
FROM attempts WHERE answered_at >= ?;

-- name: ListAttemptsByQuestionIDs :many
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at FROM attempts WHERE question_id IN (sqlc.slice('question_ids'));

-- name: ListAttemptsByQuestionIDsSince :many
SELECT id, session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at FROM attempts WHERE question_id IN (sqlc.slice('question_ids')) AND answered_at >= ?;

-- name: FailCounts :many
SELECT question_id, COUNT(*) AS n FROM attempts WHERE correct = 0 AND question_id IN (sqlc.slice('question_ids')) GROUP BY question_id;

-- name: DeleteAttemptsForQuestions :exec
DELETE FROM attempts WHERE question_id IN (sqlc.slice('question_ids'));

-- name: DeleteAttemptsBySession :exec
DELETE FROM attempts WHERE session_id = ?;

-- name: DeleteAllAttempts :exec
DELETE FROM attempts;

-- Sessions --

-- name: InsertSession :one
INSERT INTO sessions (started_at, exam_type, exam_id, topic, part, mode, order_strategy,
                       time_limit_seconds, question_time_limit_seconds, question_limit, question_number, tags, unanswered)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: EndSession :exec
UPDATE sessions SET ended_at = ?, exit_reason = ? WHERE id = ?;

-- name: DeleteSessionByID :execrows
DELETE FROM sessions WHERE id = ?;

-- name: SessionParamsByID :one
SELECT exam_type, exam_id, topic, part, mode, order_strategy,
       time_limit_seconds, question_time_limit_seconds,
       question_limit, question_number, tags, unanswered
FROM sessions WHERE id = ?;

-- name: ListSessions :many
-- Not ordered here; ListSessions in repo.go sorts by StartedAt per the
-- requested direction (see the Attempts comment above for why).
SELECT s.id, s.started_at, s.ended_at, s.exam_type, s.exam_id, s.mode, s.order_strategy,
       s.time_limit_seconds, s.question_time_limit_seconds, s.exit_reason,
       COUNT(a.id) AS answered, COALESCE(SUM(CASE WHEN a.correct THEN 1 ELSE 0 END), 0) AS correct
FROM sessions s
LEFT JOIN attempts a ON a.session_id = s.id
GROUP BY s.id;

-- name: DeleteAllSessions :exec
DELETE FROM sessions;

-- SRS (Leitner scheduling) --

-- name: GetQuestionSRS :one
SELECT question_id, box, due_at, last_reviewed_at FROM question_srs WHERE question_id = ?;

-- name: UpsertQuestionSRS :exec
INSERT INTO question_srs (question_id, box, due_at, last_reviewed_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(question_id) DO UPDATE SET box = excluded.box, due_at = excluded.due_at, last_reviewed_at = excluded.last_reviewed_at;

-- name: DueQuestionIDs :many
SELECT question_id FROM question_srs WHERE due_at <= ?;

-- name: DeleteQuestionSRSForQuestions :exec
DELETE FROM question_srs WHERE question_id IN (sqlc.slice('question_ids'));

-- name: DeleteAllQuestionSRS :exec
DELETE FROM question_srs;
