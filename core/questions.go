package core

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"
)

// GetNextQuestion returns a question matching filter. It never includes the
// answer or explanation — those are only ever exposed via SubmitAnswer.
//
// filter.Mode is "random" (default), "review" (spaced repetition: only
// questions due for review under a Leitner-box schedule — see
// Repository.DueQuestionIDs and core.updateSRS), "weak" (any question in
// the pool, but topics with lower accuracy are picked more often — unlike
// "review" this doesn't require a prior attempt on that exact question, so
// it also surfaces never-attempted questions in weak topics), or
// "sequential" (the lowest-numbered question in the pool, ordered by
// examID then question number — combine with filter.ExcludeIDs, e.g. the
// caller's already-answered question IDs, to advance through a full pass
// instead of getting the same first question every time).
func (c *Core) GetNextQuestion(ctx context.Context, filter QuestionFilter) (*Question, error) {
	pool := c.Bank.Questions(filter.Topic, filter.ExamID)
	pool = FilterByTags(pool, filter.Tags)
	if len(pool) == 0 {
		// Topic and examId each narrow the pool independently, but topics are
		// NOT shared across all exams (e.g. AM/subject-A exams use topics like
		// "Software Development"; PM/subject-B exams use a different taxonomy
		// like "Linked List", "Graph Traversal") — combining a topic with an
		// exam that doesn't use it is a common, silent way to end up here.
		// Report which topics that exam DOES have, so the caller doesn't have
		// to guess-and-check.
		if filter.Topic != "" && filter.ExamID != "" {
			examOnly := c.Bank.Questions("", filter.ExamID)
			if len(examOnly) > 0 {
				topics := make(map[string]struct{})
				for _, q := range examOnly {
					topics[q.Topic()] = struct{}{}
				}
				var list []string
				for t := range topics {
					list = append(list, t)
				}
				sort.Strings(list)
				return nil, fmt.Errorf("no questions match filter: exam %q has no %q questions; its topics are: %s", filter.ExamID, filter.Topic, strings.Join(list, ", "))
			}
		}
		return nil, fmt.Errorf("no questions match filter")
	}

	if len(filter.ExcludeIDs) > 0 {
		exclude := make(map[string]bool, len(filter.ExcludeIDs))
		for _, id := range filter.ExcludeIDs {
			exclude[id] = true
		}
		var filtered []*Question
		for _, q := range pool {
			if !exclude[q.GlobalID()] {
				filtered = append(filtered, q)
			}
		}
		pool = filtered
		if len(pool) == 0 {
			return nil, fmt.Errorf("no remaining questions match filter (all already excluded)")
		}
	}

	if filter.Unanswered {
		answered, err := c.AnsweredQuestionIDs(ctx, 0)
		if err != nil {
			return nil, err
		}
		var filtered []*Question
		for _, q := range pool {
			if !answered[q.GlobalID()] {
				filtered = append(filtered, q)
			}
		}
		pool = filtered
		if len(pool) == 0 {
			return nil, fmt.Errorf("no unanswered questions remain in this filter")
		}
	}

	switch {
	case strings.EqualFold(filter.Mode, "sequential"):
		sorted := make([]*Question, len(pool))
		copy(sorted, pool)
		slices.SortFunc(sorted, func(a, b *Question) int {
			if a.ExamID != b.ExamID {
				return cmp.Compare(a.ExamID, b.ExamID)
			}
			return cmp.Compare(a.ID, b.ID)
		})
		return stripAnswer(sorted[0]), nil
	case strings.EqualFold(filter.Mode, "review"):
		dueIDs, err := c.Repo.DueQuestionIDs(ctx, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		var filtered []*Question
		for _, q := range pool {
			if dueIDs[q.GlobalID()] {
				filtered = append(filtered, q)
			}
		}
		pool = filtered
		if len(pool) == 0 {
			return nil, fmt.Errorf("no questions due for review in this filter")
		}
		return stripAnswer(pool[rand.IntN(len(pool))]), nil

	case strings.EqualFold(filter.Mode, "weak"):
		topicStats, err := c.GetTopicStats(ctx, ScopeFilter{})
		if err != nil {
			return nil, err
		}
		accuracyByTopic := make(map[string]float64, len(topicStats))
		for _, s := range topicStats {
			if s.Answered > 0 {
				accuracyByTopic[s.Topic] = s.Accuracy
			}
		}
		return stripAnswer(weightedPickByTopicWeakness(pool, accuracyByTopic)), nil

	default:
		return stripAnswer(pool[rand.IntN(len(pool))]), nil
	}
}

// weightedPickByTopicWeakness picks randomly from pool, weighting each
// question inversely to its topic's known accuracy (lower accuracy = picked
// more often). Topics with no attempts yet get a moderate default weight so
// they still surface at a reasonable rate alongside known-weak topics.
func weightedPickByTopicWeakness(pool []*Question, accuracyByTopic map[string]float64) *Question {
	const (
		noDataWeight = 0.5  // topics never attempted: moderate priority
		floorWeight  = 0.05 // even a 100%-accurate topic can still come up
	)
	weights := make([]float64, len(pool))
	total := 0.0
	for i, q := range pool {
		w := noDataWeight
		if acc, ok := accuracyByTopic[q.Topic()]; ok {
			w = 1 - acc
		}
		w += floorWeight
		weights[i] = w
		total += w
	}
	r := rand.Float64() * total
	for i, w := range weights {
		r -= w
		if r <= 0 {
			return pool[i]
		}
	}
	return pool[len(pool)-1]
}

// GetQuestion looks up one question by exam ID + question number. When
// revealAnswer is false, the answer/explanation are stripped, same as
// GetNextQuestion. When true, the full question (including the correct
// answer and explanation) is returned — a deliberate, explicit escape hatch
// for reference lookups, not used by the normal practice/grading flow.
func (c *Core) GetQuestion(ctx context.Context, examID string, number int, revealAnswer bool) (*Question, error) {
	q := c.Bank.QuestionByExamAndNumber(examID, number)
	if q == nil {
		return nil, fmt.Errorf("question %s#%d not found", examID, number)
	}
	if revealAnswer {
		return q, nil
	}
	return stripAnswer(q), nil
}

func stripAnswer(q *Question) *Question {
	cp := *q
	cp.Answer = nil
	cp.SimpleAnswer = ""
	cp.SubAnswers = nil
	cp.Explanation = nil
	return &cp
}
