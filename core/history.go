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
func (c *Core) GetHistory(ctx context.Context, scope ScopeFilter, order HistoryOrder, limit int) ([]AttemptRecord, error) {
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
			r.Tags = q.Tags
		}
		records[i] = r
	}
	return records, nil
}

// GetSessions returns past practice sessions (newest/oldest first per order),
// each with its aggregate answered/correct count joined from attempts. scope
// supports ExamID and Part (matched against a session's own exam_id) —
// unlike GetHistory/GetTopicStats, Topic/Tag aren't supported here, since a
// session isn't inherently scoped to one topic.
func (c *Core) GetSessions(ctx context.Context, scope ScopeFilter, order HistoryOrder, limit int) ([]SessionRecord, error) {
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

func validateSessionScope(scope ScopeFilter) error {
	if scope.Topic != "" || len(scope.Tags) > 0 {
		return fmt.Errorf("topic/tag scope is not supported for sessions (a session isn't scoped to one topic or tag)")
	}
	if scope.Part != "" {
		p := strings.ToLower(scope.Part)
		if p != "am" && p != "pm" {
			return fmt.Errorf("invalid part %q, expected am or pm", scope.Part)
		}
	}
	return nil
}

func filterSessionsByScope(records []SessionRecord, scope ScopeFilter) []SessionRecord {
	if scope.IsEmpty() {
		return records
	}
	part := strings.ToLower(scope.Part)
	var filtered []SessionRecord
	for _, r := range records {
		if scope.ExamID != "" && r.ExamID != scope.ExamID {
			continue
		}
		if part != "" && ExamPart(r.ExamID) != part {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}
