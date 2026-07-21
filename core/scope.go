package core

import (
	"fmt"
	"strings"
)

// scopeQuestionIDs resolves a ScopeFilter to the set of matching question
// global IDs. Returns nil for an empty filter (meaning "no filter").
func (c *Core) scopeQuestionIDs(filter ScopeFilter) (map[string]struct{}, error) {
	if filter.IsEmpty() {
		return nil, nil
	}

	part := strings.ToLower(filter.Part)
	if part != "" && part != "am" && part != "pm" {
		return nil, fmt.Errorf("invalid part %q, expected am or pm", filter.Part)
	}

	pool := c.Bank.Questions(filter.Topic, filter.ExamID)
	pool = FilterByTags(pool, filter.Tags)
	if part != "" {
		var filtered []*Question
		for _, q := range pool {
			if ExamPart(q.ExamID) == part {
				filtered = append(filtered, q)
			}
		}
		pool = filtered
	}

	ids := make(map[string]struct{}, len(pool))
	for _, q := range pool {
		ids[q.GlobalID()] = struct{}{}
	}
	return ids, nil
}

// ParseScope parses ResetProgress's single-dimension scope string —
// "all", "topic:<name>", "tag:<name>", "exam:<id>", or "part:am"/"part:pm"
// — into a ScopeFilter.
func ParseScope(scope Scope) (ScopeFilter, error) {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return ScopeFilter{}, nil
	}
	kind, value, ok := strings.Cut(s, ":")
	if !ok {
		return ScopeFilter{}, fmt.Errorf("invalid scope %q, expected all|topic:<name>|tag:<name>|exam:<id>|part:<am|pm>", scope)
	}
	switch kind {
	case "topic":
		return ScopeFilter{Topic: value}, nil
	case "tag":
		return ScopeFilter{Tags: []string{value}}, nil
	case "exam":
		return ScopeFilter{ExamID: value}, nil
	case "part":
		value = strings.ToLower(value)
		if value != "am" && value != "pm" {
			return ScopeFilter{}, fmt.Errorf("invalid part %q, expected am or pm", value)
		}
		return ScopeFilter{Part: value}, nil
	default:
		return ScopeFilter{}, fmt.Errorf("invalid scope kind %q, expected topic, tag, exam, or part", kind)
	}
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
