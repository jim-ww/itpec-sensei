package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
)

func TestGetTopicStats(t *testing.T) {
	ctx := context.Background()
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))
	networksQ := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // topic Networks, answer A
	securityQ := bank.QuestionByExamAndNumber("2020A_FE-A", 2) // topic Security, answer B

	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, networksQ.GlobalID(), "A", false, 0) // correct
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, networksQ.GlobalID(), "Z", false, 0) // incorrect
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, securityQ.GlobalID(), "B", false, 0) // correct
	require.NoError(t, err)

	stats, err := c.GetTopicStats(ctx, core.ScopeFilter{})
	require.NoError(t, err)

	byTopic := map[string]core.TopicStat{}
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
	ctx := context.Background()
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))
	amQ := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // answer A
	pmQ := bank.QuestionByExamAndNumber("2020A_FE-B", 1) // answer C

	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, amQ.GlobalID(), "A", false, 0) // correct
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, pmQ.GlobalID(), "X", false, 0) // incorrect
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, pmQ.GlobalID(), "Y", false, 0) // incorrect
	require.NoError(t, err)

	stats, err := c.GetExamStats(ctx, core.ScopeFilter{})
	require.NoError(t, err)

	byExam := map[string]core.ExamStat{}
	for _, s := range stats {
		byExam[s.ExamID] = s
	}
	assert.Equal(t, 1, byExam["2020A_FE-A"].Answered)
	assert.Equal(t, 1, byExam["2020A_FE-A"].Correct)
	assert.Equal(t, 2, byExam["2020A_FE-B"].Answered)
	assert.Equal(t, 0, byExam["2020A_FE-B"].Correct)
}

func TestGetProgressSummary(t *testing.T) {
	ctx := context.Background()
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))
	amQ := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // answer A
	pmQ := bank.QuestionByExamAndNumber("2020A_FE-B", 1) // answer C

	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, amQ.GlobalID(), "A", false, 1000) // correct
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, pmQ.GlobalID(), "X", false, 2000) // incorrect
	require.NoError(t, err)

	// Force pmQ due for review right now — SubmitAnswer's own box-1 schedule
	// wouldn't be due for another 10 minutes (see core.srsBoxInterval), and
	// this test only cares about GetProgressSummary's own aggregation, not
	// the SRS scheduling curve.
	require.NoError(t, c.Repo.UpsertQuestionSRS(ctx, pmQ.GlobalID(), core.QuestionSRS{
		Box: 1, DueAt: time.Now().UTC().Add(-time.Minute), LastReviewedAt: time.Now().UTC(),
	}))

	summary, err := c.GetProgressSummary(ctx, core.ScopeFilter{}, core.PeriodAll)
	require.NoError(t, err)

	assert.Equal(t, 2, summary.Answered)
	assert.Equal(t, 1, summary.ReviewQueue)
	require.Len(t, summary.PartStats, 2)

	byPart := map[string]core.PartStat{}
	for _, p := range summary.PartStats {
		byPart[p.Part] = p
	}
	assert.Equal(t, 1, byPart["am"].Answered)
	assert.Equal(t, 1, byPart["am"].Correct)
	assert.Equal(t, 1, byPart["pm"].Answered)
	assert.Equal(t, 0, byPart["pm"].Correct)
}
