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

func TestValidateSessionScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   Scope
		wantErr bool
	}{
		{"all is valid", ScopeAll, false},
		{"empty is valid", Scope(""), false},
		{"exam scope is valid", Scope("exam:2020A_FE-A"), false},
		{"part scope is valid", Scope("part:am"), false},
		{"topic scope is rejected", Scope("topic:Networks"), true},
		{"unknown kind is rejected", Scope("bogus:x"), true},
		{"malformed scope is rejected", Scope("malformed"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSessionScope(tt.scope)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFilterSessionsByScope(t *testing.T) {
	records := []SessionRecord{
		{ID: 1, ExamID: "2020A_FE-A"},
		{ID: 2, ExamID: "2020A_FE-B"},
		{ID: 3, ExamID: "2020A_FE-A"},
	}

	tests := []struct {
		name    string
		scope   Scope
		wantIDs []int64
	}{
		{"all returns everything unchanged", ScopeAll, []int64{1, 2, 3}},
		{"empty returns everything unchanged", Scope(""), []int64{1, 2, 3}},
		{"exam scope filters by exact exam id", Scope("exam:2020A_FE-A"), []int64{1, 3}},
		{"part scope filters by exam part", Scope("part:pm"), []int64{2}},
		{"exam scope with no matches returns empty", Scope("exam:nope"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterSessionsByScope(records, tt.scope)
			var gotIDs []int64
			for _, r := range got {
				gotIDs = append(gotIDs, r.ID)
			}
			assert.Equal(t, tt.wantIDs, gotIDs)
		})
	}
}

func TestGetHistory(t *testing.T) {
	bank := newTestBank(t)
	q := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	answeredAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{}, HistoryNewestFirst, 20).
		Return([]repository.AttemptRow{
			{ID: 1, QuestionID: q.GlobalID(), Answer: "A", Correct: true, AnsweredAt: answeredAt},
		}, nil)

	c := New(bank, repo)
	records, err := c.GetHistory(context.Background(), ScopeAll, HistoryNewestFirst, 20)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "2020A_FE-A", records[0].ExamID)
	assert.Equal(t, "Networks", records[0].Topic)
	assert.Equal(t, "A", records[0].Answer)
}

func TestGetSessions(t *testing.T) {
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		ListSessions(context.Background(), HistoryNewestFirst).
		Return([]SessionRecord{
			{ID: 1, ExamID: "2020A_FE-A"},
			{ID: 2, ExamID: "2020A_FE-B"},
		}, nil)

	c := New(newTestBank(t), repo)
	records, err := c.GetSessions(context.Background(), Scope("exam:2020A_FE-B"), HistoryNewestFirst, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(2), records[0].ID)
}

func TestGetSessionsRejectsTopicScope(t *testing.T) {
	c := New(newTestBank(t), mocks.NewMockRepository(t))
	_, err := c.GetSessions(context.Background(), Scope("topic:Networks"), HistoryNewestFirst, 0)
	assert.Error(t, err)
}
