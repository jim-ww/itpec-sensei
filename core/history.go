package core

import (
	"context"
	"fmt"
	"strings"
)

// GetHistory returns attempts matching scope, ordered by order (newest or
// oldest first), capped at limit (0 means no cap). Each record is joined with
// question metadata (topic, examId) at query time, since attempts only store
// the raw question id.
func (c *Core) GetHistory(ctx context.Context, scope Scope, order HistoryOrder, limit int) ([]AttemptRecord, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}
	rows, err := c.Repo.ListAttempts(ctx, AttemptFilter{QuestionIDs: questionIDList(ids)}, order, limit)
	if err != nil {
		return nil, err
	}

	records := make([]AttemptRecord, len(rows))
	for i, a := range rows {
		r := AttemptRecord{
			QuestionID:  a.QuestionID,
			Answer:      a.Answer,
			Correct:     a.Correct,
			TimedOut:    a.TimedOut,
			TimeTakenMs: a.TimeTakenMs,
			AnsweredAt:  a.AnsweredAt,
		}
		if q := c.Bank.Question(a.QuestionID); q != nil {
			r.ExamID = q.ExamID
			r.Topic = q.Topic()
		}
		records[i] = r
	}
	return records, nil
}

// GetSessions returns past practice sessions (newest/oldest first per order),
// each with its aggregate answered/correct count joined from attempts. scope
// supports "all", "exam:<id>", and "part:am|pm" (matched against a session's
// own exam_id) — unlike GetHistory/GetTopicStats, "topic:<name>" isn't
// supported here, since a session isn't inherently scoped to one topic.
func (c *Core) GetSessions(ctx context.Context, scope Scope, order HistoryOrder, limit int) ([]SessionRecord, error) {
	if err := validateSessionScope(scope); err != nil {
		return nil, err
	}

	records, err := c.Repo.ListSessions(ctx, order)
	if err != nil {
		return nil, err
	}

	records = filterSessionsByScope(records, scope)
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}
	return records, nil
}

func validateSessionScope(scope Scope) error {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return nil
	}
	kind, _, ok := strings.Cut(s, ":")
	if !ok {
		return fmt.Errorf("invalid scope %q, expected all|exam:<id>|part:<am|pm>", scope)
	}
	switch kind {
	case "exam", "part":
		return nil
	case "topic", "tag":
		return fmt.Errorf("%s scope is not supported for sessions (a session isn't scoped to one %s)", kind, kind)
	default:
		return fmt.Errorf("invalid scope kind %q, expected exam or part", kind)
	}
}

func filterSessionsByScope(records []SessionRecord, scope Scope) []SessionRecord {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return records
	}
	kind, value, _ := strings.Cut(s, ":")
	var filtered []SessionRecord
	for _, r := range records {
		switch kind {
		case "exam":
			if r.ExamID == value {
				filtered = append(filtered, r)
			}
		case "part":
			if ExamPart(r.ExamID) == strings.ToLower(value) {
				filtered = append(filtered, r)
			}
		}
	}
	return filtered
}
