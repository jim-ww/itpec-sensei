package core_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
)

func TestGetHistory(t *testing.T) {
	ctx := context.Background()
	bank := newTestBank(t)
	c := core.New(bank, newTestRepo(t))
	q := bank.QuestionByExamAndNumber("2020A_FE-A", 1)

	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, q.GlobalID(), "A", false, 0)
	require.NoError(t, err)

	records, err := c.GetHistory(ctx, core.ScopeAll, core.HistoryNewestFirst, 20)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "2020A_FE-A", records[0].ExamID)
	assert.Equal(t, "Networks", records[0].Topic)
	assert.Equal(t, "A", records[0].Answer)
}

func TestGetSessions(t *testing.T) {
	ctx := context.Background()
	c := core.New(newTestBank(t), newTestRepo(t))

	_, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", ExamID: "2020A_FE-A", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	id2, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", ExamID: "2020A_FE-B", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)

	records, err := c.GetSessions(ctx, core.Scope("exam:2020A_FE-B"), core.HistoryNewestFirst, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, id2, records[0].ID)
}

func TestGetSessionsRejectsTopicScope(t *testing.T) {
	c := core.New(newTestBank(t), newTestRepo(t))
	_, err := c.GetSessions(context.Background(), core.Scope("topic:Networks"), core.HistoryNewestFirst, 0)
	assert.Error(t, err)
}
