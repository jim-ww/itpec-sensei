package core

import (
	"context"
	"fmt"
)

// StartSession creates a new sessions row — storing the full param set so
// its pool can later be recomputed to resume where it left off (see
// GetSessionParams, AnsweredQuestionIDs) or drawn fresh with the same
// filters (--repeat) — and returns its ID.
func (c *Core) StartSession(ctx context.Context, p SessionParams) (int64, error) {
	return c.Repo.InsertSession(ctx, p)
}

// EndSession marks a session finished with the given exit reason.
func (c *Core) EndSession(ctx context.Context, sessionID int64, exitReason string) error {
	return c.Repo.EndSession(ctx, sessionID, exitReason)
}

// DeleteSession permanently deletes one session and all of its attempts.
// Returns an error if the session doesn't exist.
func (c *Core) DeleteSession(ctx context.Context, sessionID int64) error {
	return c.Repo.DeleteSession(ctx, sessionID)
}

// IncompleteSessions returns sessions that never finished cleanly — either
// explicitly interrupted (user quit) or abandoned (process killed before
// EndSession ran, leaving exit_reason unset) — newest first, so the CLI can
// offer to resume them via --continue. Sessions that ran to completion have
// exit_reason "completed" and are excluded.
func (c *Core) IncompleteSessions(ctx context.Context, limit int) ([]SessionRecord, error) {
	all, err := c.GetSessions(ctx, ScopeAll, HistoryNewestFirst, 0)
	if err != nil {
		return nil, err
	}
	var out []SessionRecord
	for _, s := range all {
		if s.ExitReason == "completed" {
			continue
		}
		out = append(out, s)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// GetSessionParams loads one session's stored parameters and exact planned
// question order, for --continue (resume) and --repeat (fresh draw, same
// filters).
func (c *Core) GetSessionParams(ctx context.Context, sessionID int64) (SessionParams, error) {
	return c.Repo.SessionParamsByID(ctx, sessionID)
}

// AnsweredQuestionIDs returns the set of question GlobalIDs already
// answered within one session (so --continue can skip them when resuming),
// or across all sessions if sessionID is 0 (see QuestionFilter.Unanswered).
func (c *Core) AnsweredQuestionIDs(ctx context.Context, sessionID int64) (map[string]bool, error) {
	rows, err := c.Repo.ListAttempts(ctx, AttemptFilter{SessionID: sessionID}, "", 0)
	if err != nil {
		return nil, fmt.Errorf("query answered questions: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, a := range rows {
		out[a.QuestionID] = true
	}
	return out, nil
}
