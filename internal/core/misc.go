package core

import "context"

// ListTopics returns all topics present in the question bank.
func (c *Core) ListTopics(ctx context.Context) ([]string, error) {
	return c.Bank.Topics(), nil
}

// ListTopicsByPart returns all topics present in the question bank, grouped
// by exam part (AM vs PM), so callers can present a filterable list without
// grouping unrelated AM/PM topics together. See Bank.TopicsByPart.
func (c *Core) ListTopicsByPart(ctx context.Context) (am, pm, other []string, err error) {
	am, pm, other = c.Bank.TopicsByPart()
	return am, pm, other, nil
}

// ListExams returns all exam IDs present in the question bank.
func (c *Core) ListExams(ctx context.Context) ([]string, error) {
	return c.Bank.Exams(), nil
}

// ResetProgress deletes attempts (and their sessions where scope is "all") matching scope.
func (c *Core) ResetProgress(ctx context.Context, scope Scope) error {
	if scope == ScopeAll {
		return c.Repo.ResetAllProgress(ctx)
	}

	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return c.Repo.DeleteAttemptsForQuestions(ctx, questionIDList(ids))
}

// FailCounts returns, for the given question global IDs, how many times each has
// been answered incorrectly (used by the fail-count order strategy).
func (c *Core) FailCounts(ctx context.Context, ids []string) (map[string]int, error) {
	return c.Repo.FailCounts(ctx, ids)
}
