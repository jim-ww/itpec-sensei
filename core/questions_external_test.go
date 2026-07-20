package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
)

func TestGetNextQuestionDefaultMode(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	q, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-A"})
	require.NoError(t, err)
	assert.Equal(t, "2020A_FE-A", q.ExamID)
	// Answer must be stripped in the default (grading) flow.
	assert.Nil(t, q.Answer)
	assert.Empty(t, q.SimpleAnswer)
	assert.Nil(t, q.SubAnswers)
	assert.Nil(t, q.Explanation)
}

func TestGetNextQuestionEmptyPoolReportsExamTopics(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	_, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-A", Topic: "Databases"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Networks")
	assert.Contains(t, err.Error(), "Security")
}

func TestGetNextQuestionNoMatchAtAll(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	_, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "no-such-exam"})
	assert.Error(t, err)
}

func TestGetNextQuestionReviewMode(t *testing.T) {
	ctx := context.Background()
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)

	require.NoError(t, c.Repo.UpsertQuestionSRS(ctx, q1.GlobalID(), core.QuestionSRS{
		Box: 1, DueAt: time.Now().UTC().Add(-time.Minute), LastReviewedAt: time.Now().UTC(),
	}))

	got, err := c.GetNextQuestion(ctx, core.QuestionFilter{ExamID: "2020A_FE-A", Mode: "review"})
	require.NoError(t, err)
	assert.Equal(t, q1.GlobalID(), got.GlobalID())
	assert.NotEqual(t, q2.GlobalID(), got.GlobalID())
}

func TestGetNextQuestionReviewModeEmptyQueue(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	_, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-A", Mode: "review"})
	assert.Error(t, err)
}

func TestGetNextQuestionSequentialMode(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	c := core.New(bank, newTestRepo(t))

	got, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-A", Mode: "sequential"})
	require.NoError(t, err)
	assert.Equal(t, q1.GlobalID(), got.GlobalID())

	got, err = c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-A", Mode: "sequential", ExcludeIDs: []string{q1.GlobalID()}})
	require.NoError(t, err)
	assert.Equal(t, q2.GlobalID(), got.GlobalID())
}

func TestGetNextQuestionExcludeIDsExhausted(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-B", 1)
	c := core.New(bank, newTestRepo(t))

	_, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-B", ExcludeIDs: []string{q1.GlobalID()}})
	assert.Error(t, err)
}

func TestGetNextQuestionWeakMode(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	q, err := c.GetNextQuestion(context.Background(), core.QuestionFilter{ExamID: "2020A_FE-A", Mode: "weak"})
	require.NoError(t, err)
	assert.Equal(t, "2020A_FE-A", q.ExamID)
}

func TestGetQuestion(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	t.Run("default strips the answer", func(t *testing.T) {
		q, err := c.GetQuestion(context.Background(), "2020A_FE-A", 1, false)
		require.NoError(t, err)
		assert.Empty(t, q.SimpleAnswer)
	})

	t.Run("revealAnswer returns the full question", func(t *testing.T) {
		q, err := c.GetQuestion(context.Background(), "2020A_FE-A", 1, true)
		require.NoError(t, err)
		assert.Equal(t, "A", q.SimpleAnswer)
		assert.NotNil(t, q.Explanation)
	})

	t.Run("unknown question errors", func(t *testing.T) {
		_, err := c.GetQuestion(context.Background(), "2020A_FE-A", 999, false)
		assert.Error(t, err)
	})
}
