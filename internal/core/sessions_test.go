package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/internal/repository"
	"github.com/jim-ww/itpec-sensei/internal/repository/mocks"
)

func TestStartEndDeleteSession(t *testing.T) {
	params := SessionParams{ExamType: "fe", ExamID: "2020A_FE-A", Mode: "normal", OrderStrategy: "random"}

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().InsertSession(context.Background(), params).Return(int64(7), nil)
	repo.EXPECT().EndSession(context.Background(), int64(7), "completed").Return(nil)
	repo.EXPECT().DeleteSession(context.Background(), int64(7)).Return(nil)

	c := New(newTestBank(t), repo)

	id, err := c.StartSession(context.Background(), params)
	require.NoError(t, err)
	assert.Equal(t, int64(7), id)

	require.NoError(t, c.EndSession(context.Background(), id, "completed"))
	require.NoError(t, c.DeleteSession(context.Background(), id))
}

func TestIncompleteSessions(t *testing.T) {
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListSessions(context.Background(), HistoryNewestFirst).
		Return([]SessionRecord{
			{ID: 1, ExitReason: "completed"},
			{ID: 2, ExitReason: "interrupted"},
			{ID: 3, ExitReason: ""}, // abandoned: process killed before EndSession ran
			{ID: 4, ExitReason: "completed"},
		}, nil)

	c := New(newTestBank(t), repo)
	incomplete, err := c.IncompleteSessions(context.Background(), 0)
	require.NoError(t, err)

	var ids []int64
	for _, s := range incomplete {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []int64{2, 3}, ids)
}

func TestIncompleteSessionsRespectsLimit(t *testing.T) {
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListSessions(context.Background(), HistoryNewestFirst).
		Return([]SessionRecord{
			{ID: 1, ExitReason: "interrupted"},
			{ID: 2, ExitReason: "interrupted"},
			{ID: 3, ExitReason: "interrupted"},
		}, nil)

	c := New(newTestBank(t), repo)
	incomplete, err := c.IncompleteSessions(context.Background(), 2)
	require.NoError(t, err)
	assert.Len(t, incomplete, 2)
}

func TestGetSessionParams(t *testing.T) {
	want := SessionParams{ExamType: "fe", ExamID: "2020A_FE-A", Mode: "normal", OrderStrategy: "random"}

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().SessionParamsByID(context.Background(), int64(9)).Return(want, nil)

	c := New(newTestBank(t), repo)
	got, err := c.GetSessionParams(context.Background(), 9)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestAnsweredQuestionIDs(t *testing.T) {
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{SessionID: 3}, repository.HistoryOrder(""), 0).
		Return([]repository.AttemptRow{
			{ID: 1, QuestionID: "2020A_FE-A#1"},
			{ID: 2, QuestionID: "2020A_FE-A#2"},
		}, nil)

	c := New(newTestBank(t), repo)
	answered, err := c.AnsweredQuestionIDs(context.Background(), 3)
	require.NoError(t, err)
	assert.True(t, answered["2020A_FE-A#1"])
	assert.True(t, answered["2020A_FE-A#2"])
	assert.False(t, answered["2020A_FE-B#1"])
}
