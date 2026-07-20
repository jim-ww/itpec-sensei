package cmd

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/jim-ww/itpec-sensei/core"
)

// planQuestions builds the ordered, limit-applied question pool for a fresh
// session, per pf's filters. Nothing here is persisted — it's recomputed
// from pf (and current DB state, for review/weak/fail-count ordering) every
// time a session needs its pool, whether starting fresh or resuming via
// --continue (see planPool).
func planQuestions(ctx context.Context, c *core.Core, pf practiceFlags) ([]*core.Question, error) {
	ordered, err := planPool(ctx, c, pf)
	if err != nil {
		return nil, err
	}
	if pf.limit > 0 && pf.limit < len(ordered) {
		ordered = ordered[:pf.limit]
	}
	return ordered, nil
}

// sessionMode reports the coarse "normal"/"review" label stored on a
// session row for display/history purposes, derived from the single
// --order flag ("review" is both the filter and the label).
func sessionMode(pf practiceFlags) string {
	if pf.order == "review" {
		return "review"
	}
	return "normal"
}

// planPool builds the ordered question pool per pf's filters, without
// applying pf.limit — the caller decides how much of it to take (a fresh
// session takes the first pf.limit; --continue takes enough more to reach
// pf.limit given what's already been answered).
func planPool(ctx context.Context, c *core.Core, pf practiceFlags) ([]*core.Question, error) {
	var planned []*core.Question
	switch {
	case pf.question > 0:
		q := c.Bank.QuestionByExamAndNumber(pf.examID, pf.question)
		if q == nil {
			return nil, fmt.Errorf("question %s#%d not found", pf.examID, pf.question)
		}
		planned = []*core.Question{q}
	case pf.examID != "":
		planned = c.Bank.Questions("", pf.examID)
	default:
		planned = c.Bank.QuestionsForExams(c.Bank.ExamsByPart(pf.part))
	}
	if pf.topic != "" {
		planned = filterByTopic(planned, pf.topic)
	}
	// "review" is a filter (narrow to due questions), not a sort — once
	// applied, order the resulting due-pool randomly like any other pool.
	orderStrategy := pf.order
	if pf.order == "review" && pf.question == 0 {
		var err error
		planned, err = reviewFiltered(ctx, c, planned)
		if err != nil {
			return nil, err
		}
		orderStrategy = "random"
	}
	if len(planned) == 0 {
		return nil, nil
	}
	return orderQuestions(ctx, c, planned, orderStrategy)
}

// reviewFiltered narrows pool to questions currently due under the
// spaced-repetition (Leitner-box) schedule (see core.DueQuestionIDs).
func reviewFiltered(ctx context.Context, c *core.Core, pool []*core.Question) ([]*core.Question, error) {
	dueIDs, err := c.DueQuestionIDs(ctx)
	if err != nil {
		return nil, err
	}
	var filtered []*core.Question
	for _, q := range pool {
		if dueIDs[q.GlobalID()] {
			filtered = append(filtered, q)
		}
	}
	return filtered, nil
}

func filterByTopic(pool []*core.Question, topic string) []*core.Question {
	var filtered []*core.Question
	for _, q := range pool {
		if q.Topic() == topic {
			filtered = append(filtered, q)
		}
	}
	return filtered
}

func orderQuestions(ctx context.Context, c *core.Core, pool []*core.Question, order string) ([]*core.Question, error) {
	ordered := make([]*core.Question, len(pool))
	copy(ordered, pool)

	switch order {
	case "sequential":
		slices.SortFunc(ordered, func(a, b *core.Question) int {
			if a.ExamID != b.ExamID {
				return cmp.Compare(a.ExamID, b.ExamID)
			}
			return cmp.Compare(a.ID, b.ID)
		})
	case "random":
		shuffle(ordered)
	case "fail-count":
		ids := make([]string, len(ordered))
		for i, q := range ordered {
			ids[i] = q.GlobalID()
		}
		failCounts, err := c.FailCounts(ctx, ids)
		if err != nil {
			return nil, err
		}
		slices.SortFunc(ordered, func(a, b *core.Question) int {
			return cmp.Compare(failCounts[b.GlobalID()], failCounts[a.GlobalID()])
		})
	case "weak":
		// Weight towards topics with lower accuracy, including ones never
		// attempted yet (default weight below any known accuracy) — unlike
		// fail-count this is topic-level, not tied to a specific
		// question having been seen before.
		topicStats, err := c.GetTopicStats(ctx, core.ScopeAll)
		if err != nil {
			return nil, err
		}
		accuracyByTopic := make(map[string]float64, len(topicStats))
		for _, s := range topicStats {
			if s.Answered > 0 {
				accuracyByTopic[s.Topic] = s.Accuracy
			}
		}
		const noDataWeight = 0.5
		weight := func(q *core.Question) float64 {
			if acc, ok := accuracyByTopic[q.Topic()]; ok {
				return acc
			}
			return noDataWeight
		}
		shuffle(ordered) // randomize within topics of equal weakness
		slices.SortStableFunc(ordered, func(a, b *core.Question) int { return cmp.Compare(weight(a), weight(b)) })
	default:
		return nil, fmt.Errorf("unknown order strategy %q", order)
	}
	return ordered, nil
}

func shuffle(qs []*core.Question) {
	rand.Shuffle(len(qs), func(i, j int) { qs[i], qs[j] = qs[j], qs[i] })
}
