package core_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
)

func TestSubmitAnswer(t *testing.T) {
	bank := newTestBank(t)
	q := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // SimpleAnswer "A"
	require.NotNil(t, q)

	tests := []struct {
		name         string
		answer       string
		wantCorrect  bool
		wantRecorded string
	}{
		{"correct answer", "A", true, "A"},
		{"incorrect answer", "B", false, "B"},
		{"case insensitive correct", "a", true, "a"},
		{"idk sentinel", "idk", false, "idk"},
		{"question-mark sentinel normalizes to idk", "?", false, "idk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c := core.New(bank, newTestRepo(t))
			sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
			require.NoError(t, err)

			res, err := c.SubmitAnswer(ctx, sessionID, q.GlobalID(), tt.answer, false, 0)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCorrect, res.Correct)
			assert.Equal(t, "A", res.CorrectAnswer)

			rows, err := c.Repo.ListAttempts(ctx, core.AttemptFilter{SessionID: sessionID}, "", 0)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, tt.wantRecorded, rows[0].Answer)
			assert.Equal(t, tt.wantCorrect, rows[0].Correct)
		})
	}
}

func TestSubmitAnswerUnknownQuestion(t *testing.T) {
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	_, err := c.SubmitAnswer(context.Background(), 1, "no-such-exam#99", "A", false, 0)
	assert.Error(t, err)
}

func TestUndoLastAnswer(t *testing.T) {
	bank := newTestBank(t)
	q := bank.QuestionByExamAndNumber("2020A_FE-A", 1)

	t.Run("undoes the most recent attempt", func(t *testing.T) {
		ctx := context.Background()
		c := core.New(bank, newTestRepo(t))
		sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
		require.NoError(t, err)
		_, err = c.SubmitAnswer(ctx, sessionID, q.GlobalID(), "A", false, 0)
		require.NoError(t, err)

		r, err := c.UndoLastAnswer(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, q.GlobalID(), r.QuestionID)
		assert.Equal(t, "2020A_FE-A", r.ExamID)
		assert.Equal(t, "Networks", r.Topic)
		assert.True(t, r.Correct)
	})

	t.Run("errors when there is nothing to undo", func(t *testing.T) {
		c := core.New(bank, newTestRepo(t))
		_, err := c.UndoLastAnswer(context.Background(), 0)
		assert.Error(t, err)
	})
}
