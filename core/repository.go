package core

import (
	"context"
	"time"
)

// HistoryOrder selects chronological direction for ListAttempts/ListSessions.
type HistoryOrder string

const (
	HistoryNewestFirst HistoryOrder = "newest"
	HistoryOldestFirst HistoryOrder = "oldest"
)

// AttemptFilter narrows ListAttempts. The zero value matches every attempt.
// Fields combine with AND when set.
type AttemptFilter struct {
	QuestionIDs []string   // nil = no filter by question
	SessionID   int64      // 0 = no filter by session
	Since       *time.Time // nil = no time floor
}

// AttemptRow is one raw attempts-table row, with no question-bank join —
// callers that need topic/examId (which live in the bank, not the DB) look
// those up themselves.
type AttemptRow struct {
	ID          int64
	QuestionID  string
	Answer      string
	Correct     bool
	TimedOut    bool
	TimeTakenMs int
	AnsweredAt  time.Time
}

// SessionRecord is one past practice session, with its aggregate score
// computed by joining back to its attempts.
type SessionRecord struct {
	ID                       int64      `json:"id"`
	StartedAt                time.Time  `json:"startedAt"`
	EndedAt                  *time.Time `json:"endedAt,omitempty"`
	ExamType                 string     `json:"examType"`
	ExamID                   string     `json:"examId,omitempty"`
	Mode                     string     `json:"mode"`
	OrderStrategy            string     `json:"orderStrategy"`
	TimeLimitSeconds         *int       `json:"timeLimitSeconds,omitempty"`
	QuestionTimeLimitSeconds *int       `json:"questionTimeLimitSeconds,omitempty"`
	ExitReason               string     `json:"exitReason,omitempty"`
	Answered                 int        `json:"answered"`
	Correct                  int        `json:"correct"`
}

// SessionParams is the full set of parameters a practice session was planned
// with. Stored at session start so its pool can later be recomputed
// identically-scoped, either to resume exactly where it left off
// (--continue, minus whatever's already answered) or to draw fresh
// (--repeat). The pool itself is never stored — see cmd.planPool.
type SessionParams struct {
	ExamType                 string
	ExamID                   string
	Topic                    string
	Tags                     []string
	Part                     string
	Mode                     string
	OrderStrategy            string
	QuestionLimit            int // 0 = no limit
	QuestionNumber           int // 0 = not a single-question session
	TimeLimitSeconds         *int
	QuestionTimeLimitSeconds *int
}

// Repository is the progress store's persistence boundary: every SQL
// statement Core needs lives behind this interface. Business logic (grading,
// scoping, stats aggregation, session resume/repeat) lives in Core and
// depends only on this interface, not on any concrete database — see
// package sqlite for the real implementation, and swap in a fake Repository
// to unit test Core's logic without a database at all.
type Repository interface {
	// InsertAttempt records one graded answer against sessionID.
	InsertAttempt(ctx context.Context, sessionID int64, questionID, answer string, correct, timedOut bool, timeTakenMs int, answeredAt time.Time) error
	// ListAttempts returns attempts matching filter. order == "" means
	// unspecified order; limit == 0 means no cap.
	ListAttempts(ctx context.Context, filter AttemptFilter, order HistoryOrder, limit int) ([]AttemptRow, error)
	// DeleteAttempt deletes one attempt by its row id.
	DeleteAttempt(ctx context.Context, id int64) error
	// DueQuestionIDs returns the set of question ids whose SRS schedule
	// (see QuestionSRS) has due_at <= asOf. Questions never answered have no
	// SRS row and are never included, regardless of asOf.
	DueQuestionIDs(ctx context.Context, asOf time.Time) (map[string]bool, error)
	// GetQuestionSRS returns the SRS scheduling state for questionID, and
	// found=false if it has never been answered (no state yet).
	GetQuestionSRS(ctx context.Context, questionID string) (state QuestionSRS, found bool, err error)
	// UpsertQuestionSRS creates or overwrites questionID's SRS scheduling
	// state.
	UpsertQuestionSRS(ctx context.Context, questionID string, state QuestionSRS) error
	// FailCounts returns, for each of questionIDs, how many incorrect attempts
	// it has.
	FailCounts(ctx context.Context, questionIDs []string) (map[string]int, error)

	// InsertSession creates a new session row from p (including its planned
	// question order) and returns its id.
	InsertSession(ctx context.Context, p SessionParams) (int64, error)
	// EndSession marks a session finished with the given exit reason.
	EndSession(ctx context.Context, sessionID int64, exitReason string) error
	// DeleteSession deletes one session and all of its attempts. Returns an
	// error if the session doesn't exist.
	DeleteSession(ctx context.Context, sessionID int64) error
	// SessionParamsByID loads one session's stored parameters and planned
	// question order. Returns an error if the session doesn't exist.
	SessionParamsByID(ctx context.Context, sessionID int64) (SessionParams, error)
	// ListSessions returns every session (with aggregate answered/correct
	// counts joined from attempts), ordered per order. Scope filtering and
	// limiting are applied by the caller.
	ListSessions(ctx context.Context, order HistoryOrder) ([]SessionRecord, error)

	// ResetAllProgress deletes every attempt, session, and SRS state.
	ResetAllProgress(ctx context.Context) error
	// DeleteAttemptsForQuestions deletes every attempt, and any SRS state,
	// against any of questionIDs.
	DeleteAttemptsForQuestions(ctx context.Context, questionIDs []string) error
}

// QuestionSRS is one question's Leitner-box scheduling state.
type QuestionSRS struct {
	Box            int       // 1..maxBox (see srsMaxBox)
	DueAt          time.Time // don't resurface before this time
	LastReviewedAt time.Time
}
