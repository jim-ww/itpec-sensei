package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/internal/repository"
	"github.com/jim-ww/itpec-sensei/internal/repository/mocks"
)

func TestGetNextQuestionDefaultMode(t *testing.T) {
	bank := newTestBank(t)
	c := New(bank, mocks.NewMockRepository(t))

	q, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A"})
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
	c := New(bank, mocks.NewMockRepository(t))

	_, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A", Topic: "Databases"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Networks")
	assert.Contains(t, err.Error(), "Security")
}

func TestGetNextQuestionNoMatchAtAll(t *testing.T) {
	bank := newTestBank(t)
	c := New(bank, mocks.NewMockRepository(t))

	_, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "no-such-exam"})
	assert.Error(t, err)
}

func TestGetNextQuestionReviewMode(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		DueQuestionIDs(context.Background(), mock.Anything).
		Return(map[string]bool{q1.GlobalID(): true}, nil)

	c := New(bank, repo)
	got, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A", Mode: "review"})
	require.NoError(t, err)
	assert.Equal(t, q1.GlobalID(), got.GlobalID())
	assert.NotEqual(t, q2.GlobalID(), got.GlobalID())
}

func TestGetNextQuestionReviewModeEmptyQueue(t *testing.T) {
	bank := newTestBank(t)
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().DueQuestionIDs(context.Background(), mock.Anything).Return(map[string]bool{}, nil)

	c := New(bank, repo)
	_, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A", Mode: "review"})
	assert.Error(t, err)
}

func TestGetNextQuestionSequentialMode(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	c := New(bank, mocks.NewMockRepository(t))

	got, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A", Mode: "sequential"})
	require.NoError(t, err)
	assert.Equal(t, q1.GlobalID(), got.GlobalID())

	got, err = c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A", Mode: "sequential", ExcludeIDs: []string{q1.GlobalID()}})
	require.NoError(t, err)
	assert.Equal(t, q2.GlobalID(), got.GlobalID())
}

func TestGetNextQuestionExcludeIDsExhausted(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-B", 1)
	c := New(bank, mocks.NewMockRepository(t))

	_, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-B", ExcludeIDs: []string{q1.GlobalID()}})
	assert.Error(t, err)
}

func TestGetNextQuestionWeakMode(t *testing.T) {
	bank := newTestBank(t)
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{}, repository.HistoryOrder(""), 0).
		Return(nil, nil)

	c := New(bank, repo)
	q, err := c.GetNextQuestion(context.Background(), QuestionFilter{ExamID: "2020A_FE-A", Mode: "weak"})
	require.NoError(t, err)
	assert.Equal(t, "2020A_FE-A", q.ExamID)
}

func TestGetQuestion(t *testing.T) {
	bank := newTestBank(t)
	c := New(bank, mocks.NewMockRepository(t))

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

func TestWeightedPickByTopicWeakness(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)

	t.Run("single-question pool always returns that question", func(t *testing.T) {
		got := weightedPickByTopicWeakness([]*Question{q1}, map[string]float64{"Networks": 0.9})
		assert.Equal(t, q1.GlobalID(), got.GlobalID())
	})

	t.Run("topic with no accuracy data still gets picked over many trials", func(t *testing.T) {
		q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2) // "Security", no accuracy data
		pool := []*Question{q1, q2}
		accuracy := map[string]float64{"Networks": 1.0} // Networks perfect, Security unknown
		seenSecurity := false
		for range 200 {
			got := weightedPickByTopicWeakness(pool, accuracy)
			if got.Topic() == "Security" {
				seenSecurity = true
				break
			}
		}
		assert.True(t, seenSecurity, "expected Security (no data) to surface at least once across 200 draws")
	})
}
