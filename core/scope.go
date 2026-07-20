package core

import (
	"fmt"
	"strings"
)

// scopeQuestionIDs resolves a Scope to the set of matching question global IDs.
// Returns nil for ScopeAll (meaning "no filter").
func (c *Core) scopeQuestionIDs(scope Scope) (map[string]struct{}, error) {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return nil, nil
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid scope %q, expected all|topic:<name>|tag:<name>|exam:<id>|part:<am|pm>", scope)
	}
	kind, value := parts[0], parts[1]
	var pool []*Question
	switch kind {
	case "topic":
		pool = c.Bank.Questions(value, "")
	case "tag":
		pool = FilterByTags(c.Bank.Questions("", ""), []string{value})
	case "exam":
		pool = c.Bank.Questions("", value)
	case "part":
		value = strings.ToLower(value)
		if value != "am" && value != "pm" {
			return nil, fmt.Errorf("invalid part %q, expected am or pm", value)
		}
		pool = c.Bank.QuestionsForExams(c.Bank.ExamsByPart(value))
	default:
		return nil, fmt.Errorf("invalid scope kind %q, expected topic, tag, exam, or part", kind)
	}
	ids := make(map[string]struct{}, len(pool))
	for _, q := range pool {
		ids[q.GlobalID()] = struct{}{}
	}
	return ids, nil
}

// questionIDList converts a scopeQuestionIDs result to the slice form
// AttemptFilter.QuestionIDs expects: nil (no filter) stays nil; a non-nil-but-
// possibly-empty set becomes a (possibly empty) slice, so an empty scope
// match correctly filters to zero rows rather than being mistaken for "no
// filter".
func questionIDList(ids map[string]struct{}) []string {
	if ids == nil {
		return nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	return list
}
