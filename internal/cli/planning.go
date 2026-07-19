package cli

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// planQuestions builds the ordered, limit-applied question pool for a fresh
// session, per pf's filters.
func planQuestions(ctx context.Context, c *core.Core, pf practiceFlags) ([]*core.Question, error) {
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
	if pf.mode == "review" && pf.question == 0 {
		var err error
		planned, err = reviewFiltered(ctx, c, planned)
		if err != nil {
			return nil, err
		}
	}
	if len(planned) == 0 {
		return nil, nil
	}

	ordered, err := orderQuestions(ctx, c, planned, pf.order)
	if err != nil {
		return nil, err
	}

	if pf.limit > 0 && pf.limit < len(ordered) {
		ordered = ordered[:pf.limit]
	}
	return ordered, nil
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

func globalIDs(qs []*core.Question) []string {
	ids := make([]string, len(qs))
	for i, q := range qs {
		ids[i] = q.GlobalID()
	}
	return ids
}

func orderQuestions(ctx context.Context, c *core.Core, pool []*core.Question, order string) ([]*core.Question, error) {
	ordered := make([]*core.Question, len(pool))
	copy(ordered, pool)

	switch order {
	case "sequential":
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].ExamID != ordered[j].ExamID {
				return ordered[i].ExamID < ordered[j].ExamID
			}
			return ordered[i].ID < ordered[j].ID
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
		sort.Slice(ordered, func(i, j int) bool {
			return failCounts[ordered[i].GlobalID()] > failCounts[ordered[j].GlobalID()]
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
		sort.SliceStable(ordered, func(i, j int) bool { return weight(ordered[i]) < weight(ordered[j]) })
	default:
		return nil, fmt.Errorf("unknown order strategy %q", order)
	}
	return ordered, nil
}

func shuffle(qs []*core.Question) {
	rand.Shuffle(len(qs), func(i, j int) { qs[i], qs[j] = qs[j], qs[i] })
}
