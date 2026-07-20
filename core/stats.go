package core

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"
)

// GetProgressSummary computes overall accuracy/streak/heatmap/review-queue for scope+period.
func (c *Core) GetProgressSummary(ctx context.Context, scope Scope, period Period) (*ProgressSummary, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}

	filter := AttemptFilter{QuestionIDs: questionIDList(ids)}
	if since := periodSince(period); since != nil {
		filter.Since = since
	}
	rows, err := c.Repo.ListAttempts(ctx, filter, "", 0)
	if err != nil {
		return nil, err
	}

	summary := &ProgressSummary{Heatmap: make(map[string]int)}
	type partAcc struct {
		answered, correct int
		times             []int
	}
	byPart := make(map[string]*partAcc)
	for _, a := range rows {
		summary.Answered++
		day := a.AnsweredAt.Format("2006-01-02")
		summary.Heatmap[day]++

		part := "other"
		if q := c.Bank.Question(a.QuestionID); q != nil {
			if p := ExamPart(q.ExamID); p != "" {
				part = p
			}
		}
		acc := byPart[part]
		if acc == nil {
			acc = &partAcc{}
			byPart[part] = acc
		}
		acc.answered++
		if a.Correct {
			acc.correct++
		}
		if a.TimeTakenMs > 0 {
			acc.times = append(acc.times, a.TimeTakenMs)
		}
	}
	summary.Streak = computeStreak(summary.Heatmap)
	summary.MaxStreak = computeMaxStreak(summary.Heatmap)

	for _, part := range []string{"am", "pm", "other"} {
		acc := byPart[part]
		if acc == nil {
			continue
		}
		ps := PartStat{Part: part, Answered: acc.answered, Correct: acc.correct}
		if acc.answered > 0 {
			ps.Accuracy = float64(acc.correct) / float64(acc.answered)
		}
		ps.AvgTimeMs, ps.MedianTimeMs = timeStats(acc.times)
		summary.PartStats = append(summary.PartStats, ps)
	}

	reviewIDs, err := c.Repo.DueQuestionIDs(ctx, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if ids != nil {
		count := 0
		for id := range reviewIDs {
			if _, ok := ids[id]; ok {
				count++
			}
		}
		summary.ReviewQueue = count
	} else {
		summary.ReviewQueue = len(reviewIDs)
	}

	return summary, nil
}

// timeStats returns the mean and median of ms, or (0, 0) if ms is empty.
func timeStats(ms []int) (avg, median float64) {
	if len(ms) == 0 {
		return 0, 0
	}
	sum := 0
	for _, v := range ms {
		sum += v
	}
	avg = float64(sum) / float64(len(ms))

	sorted := append([]int(nil), ms...)
	sort.Ints(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		median = float64(sorted[mid-1]+sorted[mid]) / 2
	} else {
		median = float64(sorted[mid])
	}
	return avg, median
}

func computeStreak(heatmap map[string]int) int {
	streak := 0
	day := time.Now().UTC()
	// If today has no activity yet, start counting from yesterday.
	if heatmap[day.Format("2006-01-02")] == 0 {
		day = day.AddDate(0, 0, -1)
	}
	for {
		key := day.Format("2006-01-02")
		if heatmap[key] == 0 {
			break
		}
		streak++
		day = day.AddDate(0, 0, -1)
	}
	return streak
}

// computeMaxStreak returns the longest run of consecutive days with any
// recorded activity, over the entire heatmap — unlike computeStreak, this
// isn't anchored to "today" and doesn't reset once the current streak breaks.
func computeMaxStreak(heatmap map[string]int) int {
	if len(heatmap) == 0 {
		return 0
	}
	days := make([]time.Time, 0, len(heatmap))
	for k, count := range heatmap {
		if count == 0 {
			continue
		}
		t, err := time.Parse("2006-01-02", k)
		if err != nil {
			continue
		}
		days = append(days, t)
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })

	max, cur := 0, 0
	for i, d := range days {
		if i == 0 || d.Sub(days[i-1]) != 24*time.Hour {
			cur = 1
		} else {
			cur++
		}
		if cur > max {
			max = cur
		}
	}
	return max
}

// periodSince returns the cutoff time for period, or nil for PeriodAll (no floor).
func periodSince(period Period) *time.Time {
	var t time.Time
	switch period {
	case PeriodWeek:
		t = time.Now().UTC().AddDate(0, 0, -7)
	case PeriodMonth:
		t = time.Now().UTC().AddDate(0, -1, 0)
	default:
		return nil
	}
	return &t
}

// GetTopicStats returns per-topic answered/correct/accuracy for scope.
func (c *Core) GetTopicStats(ctx context.Context, scope Scope) ([]TopicStat, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}
	rows, err := c.Repo.ListAttempts(ctx, AttemptFilter{QuestionIDs: questionIDList(ids)}, "", 0)
	if err != nil {
		return nil, err
	}

	type acc struct{ answered, correct int }
	byTopic := make(map[string]*acc)
	for _, a := range rows {
		topic := "Uncategorized"
		if q := c.Bank.Question(a.QuestionID); q != nil {
			topic = q.Topic()
		}
		t := byTopic[topic]
		if t == nil {
			t = &acc{}
			byTopic[topic] = t
		}
		t.answered++
		if a.Correct {
			t.correct++
		}
	}

	var stats []TopicStat
	for topic, a := range byTopic {
		s := TopicStat{Topic: topic, Answered: a.answered, Correct: a.correct}
		if a.answered > 0 {
			s.Accuracy = float64(a.correct) / float64(a.answered)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// GetExamStats returns per-exam answered/correct/accuracy for scope, mirroring GetTopicStats.
func (c *Core) GetExamStats(ctx context.Context, scope Scope) ([]ExamStat, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}
	rows, err := c.Repo.ListAttempts(ctx, AttemptFilter{QuestionIDs: questionIDList(ids)}, "", 0)
	if err != nil {
		return nil, err
	}

	type acc struct{ answered, correct int }
	byExam := make(map[string]*acc)
	for _, a := range rows {
		examID := "Unknown"
		if q := c.Bank.Question(a.QuestionID); q != nil {
			examID = q.ExamID
		}
		e := byExam[examID]
		if e == nil {
			e = &acc{}
			byExam[examID] = e
		}
		e.answered++
		if a.Correct {
			e.correct++
		}
	}

	var stats []ExamStat
	for examID, a := range byExam {
		s := ExamStat{ExamID: examID, Answered: a.answered, Correct: a.correct}
		if a.answered > 0 {
			s.Accuracy = float64(a.correct) / float64(a.answered)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// GetExam returns readable metadata (name, date, duration, question count)
// for one exam ID plus the user's own progress on it, or an error if the
// exam ID is unknown.
func (c *Core) GetExam(ctx context.Context, examID string) (*ExamDetail, error) {
	info, ok := c.Bank.ExamInfo(examID)
	if !ok {
		return nil, fmt.Errorf("exam %q not found", examID)
	}
	detail := &ExamDetail{
		ExamID:          examID,
		Name:            info.Exam,
		Date:            info.Date,
		Part:            ExamPart(examID),
		DurationMinutes: info.DurationMinutes,
		TotalQuestions:  info.TotalQuestions,
	}
	if info.DurationMinutes > 0 && info.TotalQuestions > 0 {
		detail.TargetSecondsPerQuestion = info.DurationMinutes * 60 / info.TotalQuestions
	}

	stats, err := c.GetExamStats(ctx, Scope("exam:"+examID))
	if err != nil {
		return nil, err
	}
	if len(stats) > 0 {
		detail.Answered = stats[0].Answered
		detail.Correct = stats[0].Correct
		detail.Accuracy = stats[0].Accuracy
	}

	ids, err := c.scopeQuestionIDs(Scope("exam:" + examID))
	if err != nil {
		return nil, err
	}
	rows, err := c.Repo.ListAttempts(ctx, AttemptFilter{QuestionIDs: questionIDList(ids)}, "", 0)
	if err != nil {
		return nil, err
	}
	var times []int
	for _, a := range rows {
		if a.TimeTakenMs > 0 {
			times = append(times, a.TimeTakenMs)
		}
	}
	detail.AvgTimeMs, _ = timeStats(times)

	return detail, nil
}
