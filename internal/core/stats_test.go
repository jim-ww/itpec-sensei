package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/internal/repository"
	"github.com/jim-ww/itpec-sensei/internal/repository/mocks"
)

func TestTimeStats(t *testing.T) {
	tests := []struct {
		name       string
		ms         []int
		wantAvg    float64
		wantMedian float64
	}{
		{"empty returns zero", nil, 0, 0},
		{"single value", []int{100}, 100, 100},
		{"odd count uses middle element", []int{100, 200, 300}, 200, 200},
		{"even count averages the two middle elements", []int{100, 200, 300, 400}, 250, 250},
		{"unsorted input still sorted for median", []int{300, 100, 200}, 200, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avg, median := timeStats(tt.ms)
			assert.Equal(t, tt.wantAvg, avg)
			assert.Equal(t, tt.wantMedian, median)
		})
	}
}

func day(offsetFromToday int) string {
	return time.Now().UTC().AddDate(0, 0, offsetFromToday).Format("2006-01-02")
}

func TestComputeStreak(t *testing.T) {
	tests := []struct {
		name    string
		heatmap map[string]int
		want    int
	}{
		{"no activity at all", map[string]int{}, 0},
		{"active today only", map[string]int{day(0): 1}, 1},
		{"active today and yesterday", map[string]int{day(0): 1, day(-1): 2}, 2},
		{"gap breaks the streak", map[string]int{day(0): 1, day(-2): 1}, 1},
		{"no activity today, anchors on yesterday", map[string]int{day(-1): 1, day(-2): 1}, 2},
		{"no activity today or yesterday", map[string]int{day(-2): 1}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeStreak(tt.heatmap))
		})
	}
}

func TestComputeMaxStreak(t *testing.T) {
	tests := []struct {
		name    string
		heatmap map[string]int
		want    int
	}{
		{"empty heatmap", map[string]int{}, 0},
		{"single day", map[string]int{"2026-01-01": 1}, 1},
		{"three consecutive days", map[string]int{"2026-01-01": 1, "2026-01-02": 1, "2026-01-03": 1}, 3},
		{"two separate streaks picks the longer one", map[string]int{
			"2026-01-01": 1, "2026-01-02": 1, // streak of 2
			"2026-01-10": 1, "2026-01-11": 1, "2026-01-12": 1, // streak of 3
		}, 3},
		{"zero-count days don't count as activity", map[string]int{"2026-01-01": 0, "2026-01-02": 1}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeMaxStreak(tt.heatmap))
		})
	}
}

func TestGetTopicStats(t *testing.T) {
	bank := newTestBank(t)
	networksQ := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // topic Networks
	securityQ := bank.QuestionByExamAndNumber("2020A_FE-A", 2) // topic Security

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{}, repository.HistoryOrder(""), 0).
		Return([]repository.AttemptRow{
			{ID: 1, QuestionID: networksQ.GlobalID(), Correct: true},
			{ID: 2, QuestionID: networksQ.GlobalID(), Correct: false},
			{ID: 3, QuestionID: securityQ.GlobalID(), Correct: true},
		}, nil)

	c := New(bank, repo)
	stats, err := c.GetTopicStats(context.Background(), ScopeAll)
	require.NoError(t, err)

	byTopic := map[string]TopicStat{}
	for _, s := range stats {
		byTopic[s.Topic] = s
	}
	require.Contains(t, byTopic, "Networks")
	require.Contains(t, byTopic, "Security")
	assert.Equal(t, 2, byTopic["Networks"].Answered)
	assert.Equal(t, 1, byTopic["Networks"].Correct)
	assert.InDelta(t, 0.5, byTopic["Networks"].Accuracy, 0.001)
	assert.Equal(t, 1, byTopic["Security"].Answered)
	assert.Equal(t, 1, byTopic["Security"].Correct)
	assert.InDelta(t, 1.0, byTopic["Security"].Accuracy, 0.001)
}

func TestGetExamStats(t *testing.T) {
	bank := newTestBank(t)
	amQ := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	pmQ := bank.QuestionByExamAndNumber("2020A_FE-B", 1)

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{}, repository.HistoryOrder(""), 0).
		Return([]repository.AttemptRow{
			{ID: 1, QuestionID: amQ.GlobalID(), Correct: true},
			{ID: 2, QuestionID: pmQ.GlobalID(), Correct: false},
			{ID: 3, QuestionID: pmQ.GlobalID(), Correct: false},
		}, nil)

	c := New(bank, repo)
	stats, err := c.GetExamStats(context.Background(), ScopeAll)
	require.NoError(t, err)

	byExam := map[string]ExamStat{}
	for _, s := range stats {
		byExam[s.ExamID] = s
	}
	assert.Equal(t, 1, byExam["2020A_FE-A"].Answered)
	assert.Equal(t, 1, byExam["2020A_FE-A"].Correct)
	assert.Equal(t, 2, byExam["2020A_FE-B"].Answered)
	assert.Equal(t, 0, byExam["2020A_FE-B"].Correct)
}

func TestGetProgressSummary(t *testing.T) {
	bank := newTestBank(t)
	amQ := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	pmQ := bank.QuestionByExamAndNumber("2020A_FE-B", 1)
	now := time.Now().UTC()

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{}, repository.HistoryOrder(""), 0).
		Return([]repository.AttemptRow{
			{ID: 1, QuestionID: amQ.GlobalID(), Correct: true, AnsweredAt: now, TimeTakenMs: 1000},
			{ID: 2, QuestionID: pmQ.GlobalID(), Correct: false, AnsweredAt: now, TimeTakenMs: 2000},
		}, nil)
	repo.EXPECT().ReviewQueueQuestionIDs(context.Background()).Return(map[string]bool{pmQ.GlobalID(): true}, nil)

	c := New(bank, repo)
	summary, err := c.GetProgressSummary(context.Background(), ScopeAll, PeriodAll)
	require.NoError(t, err)

	assert.Equal(t, 2, summary.Answered)
	assert.Equal(t, 1, summary.ReviewQueue)
	require.Len(t, summary.PartStats, 2)

	byPart := map[string]PartStat{}
	for _, p := range summary.PartStats {
		byPart[p.Part] = p
	}
	assert.Equal(t, 1, byPart["am"].Answered)
	assert.Equal(t, 1, byPart["am"].Correct)
	assert.Equal(t, 1, byPart["pm"].Answered)
	assert.Equal(t, 0, byPart["pm"].Correct)
}
