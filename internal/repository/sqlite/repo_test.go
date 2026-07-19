package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/internal/repository"
)

func newTestRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "progress.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewRepository(db)
}

func TestOpenAppliesEmbeddedSchema(t *testing.T) {
	repo := newTestRepo(t)
	id, err := repo.InsertSession(context.Background(), repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	assert.NotZero(t, id)
}

func TestInsertAndListAttempts(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	sessionA, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	sessionB, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.InsertAttempt(ctx, sessionA, "examA#1", "A", true, false, 1000, t0))
	require.NoError(t, repo.InsertAttempt(ctx, sessionA, "examA#2", "B", false, false, 2000, t0.Add(time.Hour)))
	require.NoError(t, repo.InsertAttempt(ctx, sessionB, "examB#1", "C", true, true, 3000, t0.Add(2*time.Hour)))

	tests := []struct {
		name    string
		filter  repository.AttemptFilter
		order   repository.HistoryOrder
		limit   int
		wantIDs []string // question ids, in expected order
	}{
		{
			name:    "filters by session",
			filter:  repository.AttemptFilter{SessionID: sessionA},
			order:   repository.HistoryNewestFirst,
			wantIDs: []string{"examA#2", "examA#1"},
		},
		{
			name:    "filters by question ids",
			filter:  repository.AttemptFilter{QuestionIDs: []string{"examA#1", "examB#1"}},
			order:   repository.HistoryOldestFirst,
			wantIDs: []string{"examA#1", "examB#1"},
		},
		{
			name:    "empty non-nil question id filter matches nothing",
			filter:  repository.AttemptFilter{QuestionIDs: []string{}},
			order:   repository.HistoryNewestFirst,
			wantIDs: nil,
		},
		{
			name:    "no filter returns everything, newest first",
			filter:  repository.AttemptFilter{},
			order:   repository.HistoryNewestFirst,
			wantIDs: []string{"examB#1", "examA#2", "examA#1"},
		},
		{
			name:    "since filters by time floor",
			filter:  repository.AttemptFilter{Since: ptrTime(t0.Add(90 * time.Minute))},
			order:   repository.HistoryNewestFirst,
			wantIDs: []string{"examB#1"},
		},
		{
			name:    "question ids combined with since",
			filter:  repository.AttemptFilter{QuestionIDs: []string{"examA#1", "examA#2"}, Since: ptrTime(t0.Add(30 * time.Minute))},
			order:   repository.HistoryNewestFirst,
			wantIDs: []string{"examA#2"},
		},
		{
			name:    "limit truncates",
			filter:  repository.AttemptFilter{},
			order:   repository.HistoryNewestFirst,
			limit:   2,
			wantIDs: []string{"examB#1", "examA#2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := repo.ListAttempts(ctx, tt.filter, tt.order, tt.limit)
			require.NoError(t, err)
			var ids []string
			for _, r := range rows {
				ids = append(ids, r.QuestionID)
			}
			assert.Equal(t, tt.wantIDs, ids)
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestDeleteAttempt(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sessionID, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	require.NoError(t, repo.InsertAttempt(ctx, sessionID, "examA#1", "A", true, false, 0, time.Now().UTC()))

	rows, err := repo.ListAttempts(ctx, repository.AttemptFilter{SessionID: sessionID}, "", 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, repo.DeleteAttempt(ctx, rows[0].ID))

	rows, err = repo.ListAttempts(ctx, repository.AttemptFilter{SessionID: sessionID}, "", 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestQuestionSRS(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, found, err := repo.GetQuestionSRS(ctx, "examA#1")
	require.NoError(t, err)
	assert.False(t, found, "never-scheduled question has no SRS state")

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// examA#1 is due tomorrow -> not due as of t0.
	require.NoError(t, repo.UpsertQuestionSRS(ctx, "examA#1", repository.QuestionSRS{
		Box: 1, DueAt: t0.Add(24 * time.Hour), LastReviewedAt: t0,
	}))
	// examA#2 was due yesterday -> due as of t0.
	require.NoError(t, repo.UpsertQuestionSRS(ctx, "examA#2", repository.QuestionSRS{
		Box: 1, DueAt: t0.Add(-24 * time.Hour), LastReviewedAt: t0.Add(-48 * time.Hour),
	}))

	ids, err := repo.DueQuestionIDs(ctx, t0)
	require.NoError(t, err)
	assert.False(t, ids["examA#1"])
	assert.True(t, ids["examA#2"])

	state, found, err := repo.GetQuestionSRS(ctx, "examA#2")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 1, state.Box)

	// Upsert overwrites existing state.
	require.NoError(t, repo.UpsertQuestionSRS(ctx, "examA#2", repository.QuestionSRS{
		Box: 3, DueAt: t0.Add(7 * 24 * time.Hour), LastReviewedAt: t0,
	}))
	state, found, err = repo.GetQuestionSRS(ctx, "examA#2")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 3, state.Box)
}

func TestFailCounts(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sessionID, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)

	require.NoError(t, repo.InsertAttempt(ctx, sessionID, "examA#1", "B", false, false, 0, time.Now().UTC()))
	require.NoError(t, repo.InsertAttempt(ctx, sessionID, "examA#1", "A", true, false, 0, time.Now().UTC()))
	require.NoError(t, repo.InsertAttempt(ctx, sessionID, "examA#2", "X", false, false, 0, time.Now().UTC()))
	require.NoError(t, repo.InsertAttempt(ctx, sessionID, "examA#2", "X", false, false, 0, time.Now().UTC()))

	counts, err := repo.FailCounts(ctx, []string{"examA#1", "examA#2", "examA#3"})
	require.NoError(t, err)
	assert.Equal(t, 1, counts["examA#1"])
	assert.Equal(t, 2, counts["examA#2"])
	assert.Zero(t, counts["examA#3"])

	empty, err := repo.FailCounts(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestSessionLifecycle(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	tl, qtl := 90*60, 30
	params := repository.SessionParams{
		ExamType: "fe", ExamID: "examA", Topic: "Networks", Part: "am",
		Mode: "normal", OrderStrategy: "random",
		QuestionLimit: 5, QuestionNumber: 0,
		TimeLimitSeconds: &tl, QuestionTimeLimitSeconds: &qtl,
	}

	id, err := repo.InsertSession(ctx, params)
	require.NoError(t, err)
	assert.NotZero(t, id)

	got, err := repo.SessionParamsByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, params, got)

	require.NoError(t, repo.EndSession(ctx, id, "completed"))

	sessions, err := repo.ListSessions(ctx, repository.HistoryNewestFirst)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "completed", sessions[0].ExitReason)
	assert.NotNil(t, sessions[0].EndedAt)

	_, err = repo.SessionParamsByID(ctx, id+999)
	assert.Error(t, err)
}

func TestListSessionsAggregatesAnsweredAndCorrect(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	s1, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	time.Sleep(time.Millisecond) // ensure a distinct started_at ordering
	s2, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)

	require.NoError(t, repo.InsertAttempt(ctx, s1, "examA#1", "A", true, false, 0, time.Now().UTC()))
	require.NoError(t, repo.InsertAttempt(ctx, s1, "examA#2", "B", false, false, 0, time.Now().UTC()))

	sessions, err := repo.ListSessions(ctx, repository.HistoryOldestFirst)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	byID := map[int64]repository.SessionRecord{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	assert.Equal(t, 2, byID[s1].Answered)
	assert.Equal(t, 1, byID[s1].Correct)
	assert.Equal(t, 0, byID[s2].Answered)
	assert.Equal(t, 0, byID[s2].Correct)

	assert.True(t, sessions[0].StartedAt.Before(sessions[1].StartedAt) || sessions[0].StartedAt.Equal(sessions[1].StartedAt))
}

func TestDeleteSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	id, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	require.NoError(t, repo.InsertAttempt(ctx, id, "examA#1", "A", true, false, 0, time.Now().UTC()))

	require.NoError(t, repo.DeleteSession(ctx, id))

	_, err = repo.SessionParamsByID(ctx, id)
	assert.Error(t, err, "session row should be gone")

	rows, err := repo.ListAttempts(ctx, repository.AttemptFilter{SessionID: id}, "", 0)
	require.NoError(t, err)
	assert.Empty(t, rows, "attempts should be deleted along with the session")

	err = repo.DeleteSession(ctx, id)
	assert.Error(t, err, "deleting an already-deleted session should error")
}

func TestResetAllProgress(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	id, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	require.NoError(t, repo.InsertAttempt(ctx, id, "examA#1", "A", true, false, 0, time.Now().UTC()))

	require.NoError(t, repo.ResetAllProgress(ctx))

	sessions, err := repo.ListSessions(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, sessions)

	rows, err := repo.ListAttempts(ctx, repository.AttemptFilter{}, "", 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestDeleteAttemptsForQuestions(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	id, err := repo.InsertSession(ctx, repository.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	require.NoError(t, repo.InsertAttempt(ctx, id, "examA#1", "A", true, false, 0, time.Now().UTC()))
	require.NoError(t, repo.InsertAttempt(ctx, id, "examA#2", "B", true, false, 0, time.Now().UTC()))

	require.NoError(t, repo.DeleteAttemptsForQuestions(ctx, []string{"examA#1"}))

	rows, err := repo.ListAttempts(ctx, repository.AttemptFilter{}, "", 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "examA#2", rows[0].QuestionID)

	// no-op on empty input
	require.NoError(t, repo.DeleteAttemptsForQuestions(ctx, nil))
}
