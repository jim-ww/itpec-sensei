package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
)

func TestStartEndDeleteSession(t *testing.T) {
	ctx := context.Background()
	params := core.SessionParams{ExamType: "fe", ExamID: "2020A_FE-A", Mode: "normal", OrderStrategy: "random"}
	c := core.New(newTestBank(t), newTestRepo(t))

	id, err := c.StartSession(ctx, params)
	require.NoError(t, err)
	assert.NotZero(t, id)

	require.NoError(t, c.EndSession(ctx, id, "completed"))
	require.NoError(t, c.DeleteSession(ctx, id))
}

func TestIncompleteSessions(t *testing.T) {
	ctx := context.Background()
	c := core.New(newTestBank(t), newTestRepo(t))
	params := core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"}

	completed1, err := c.StartSession(ctx, params)
	require.NoError(t, err)
	require.NoError(t, c.EndSession(ctx, completed1, "completed"))
	time.Sleep(time.Millisecond)

	interrupted, err := c.StartSession(ctx, params)
	require.NoError(t, err)
	require.NoError(t, c.EndSession(ctx, interrupted, "interrupted"))
	time.Sleep(time.Millisecond)

	// abandoned: never ended, so exit_reason stays unset (process killed
	// before EndSession ran).
	abandoned, err := c.StartSession(ctx, params)
	require.NoError(t, err)
	time.Sleep(time.Millisecond)

	completed2, err := c.StartSession(ctx, params)
	require.NoError(t, err)
	require.NoError(t, c.EndSession(ctx, completed2, "completed"))

	incomplete, err := c.IncompleteSessions(ctx, 0)
	require.NoError(t, err)

	var ids []int64
	for _, s := range incomplete {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []int64{abandoned, interrupted}, ids) // newest first, completed excluded
}

func TestIncompleteSessionsRespectsLimit(t *testing.T) {
	ctx := context.Background()
	c := core.New(newTestBank(t), newTestRepo(t))
	params := core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"}

	for range 3 {
		id, err := c.StartSession(ctx, params)
		require.NoError(t, err)
		require.NoError(t, c.EndSession(ctx, id, "interrupted"))
		time.Sleep(time.Millisecond)
	}

	incomplete, err := c.IncompleteSessions(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, incomplete, 2)
}

func TestGetSessionParams(t *testing.T) {
	ctx := context.Background()
	want := core.SessionParams{ExamType: "fe", ExamID: "2020A_FE-A", Mode: "normal", OrderStrategy: "random"}
	c := core.New(newTestBank(t), newTestRepo(t))

	id, err := c.StartSession(ctx, want)
	require.NoError(t, err)

	got, err := c.GetSessionParams(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestAnsweredQuestionIDs(t *testing.T) {
	ctx := context.Background()
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))

	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)

	_, err = c.SubmitAnswer(ctx, sessionID, "2020A_FE-A#1", "A", false, 0)
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, "2020A_FE-A#2", "B", false, 0)
	require.NoError(t, err)

	answered, err := c.AnsweredQuestionIDs(ctx, sessionID)
	require.NoError(t, err)
	assert.True(t, answered["2020A_FE-A#1"])
	assert.True(t, answered["2020A_FE-A#2"])
	assert.False(t, answered["2020A_FE-B#1"])
}
