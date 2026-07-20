package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
	"github.com/jim-ww/itpec-sensei/sqlite"
)

// newPlanningTestBank builds a small fixture bank: exam "2020A_FE-A" with q1
// (topic "Networks", answer "A") and q2 (topic "Security", answer "B").
func newPlanningTestBank(t *testing.T) *core.Bank {
	t.Helper()
	dir := t.TempDir()
	exam := core.ExamData{
		ExamID: "2020A_FE-A",
		Questions: []core.Question{
			{ID: 1, ImageURL: "q1.png", Explanation: &core.Explanation{Topic: "Networks"}, SimpleAnswer: "A"},
			{ID: 2, ImageURL: "q2.png", Explanation: &core.Explanation{Topic: "Security"}, SimpleAnswer: "B"},
		},
	}
	b, err := json.Marshal(exam)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, exam.ExamID+".json"), b, 0o644))

	bank, err := core.LoadBank(dir)
	require.NoError(t, err)
	return bank
}

// newPlanningTestRepo opens a real temp-file SQLite-backed Repository, so
// planning logic is exercised against actual SQL instead of a stubbed
// interface.
func newPlanningTestRepo(t *testing.T) core.Repository {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "progress.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return sqlite.NewRepository(db)
}

func TestOrderQuestionsSequential(t *testing.T) {
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q2, q1} // deliberately reversed

	c := core.New(bank, newPlanningTestRepo(t))
	ordered, err := orderQuestions(context.Background(), c, pool, "sequential")
	require.NoError(t, err)
	require.Len(t, ordered, 2)
	assert.Equal(t, 1, ordered[0].ID)
	assert.Equal(t, 2, ordered[1].ID)
}

func TestOrderQuestionsRandomPreservesSet(t *testing.T) {
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q1, q2}

	c := core.New(bank, newPlanningTestRepo(t))
	ordered, err := orderQuestions(context.Background(), c, pool, "random")
	require.NoError(t, err)
	assert.ElementsMatch(t, pool, ordered)
}

func TestOrderQuestionsFailCount(t *testing.T) {
	ctx := context.Background()
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q1, q2}

	c := core.New(bank, newPlanningTestRepo(t))
	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	_, err = c.SubmitAnswer(ctx, sessionID, q1.GlobalID(), "Z", false, 0) // wrong, once
	require.NoError(t, err)
	for range 5 {
		_, err = c.SubmitAnswer(ctx, sessionID, q2.GlobalID(), "Z", false, 0) // wrong, five times
		require.NoError(t, err)
	}

	ordered, err := orderQuestions(ctx, c, pool, "fail-count")
	require.NoError(t, err)
	require.Len(t, ordered, 2)
	// Higher fail count sorts first.
	assert.Equal(t, q2.GlobalID(), ordered[0].GlobalID())
	assert.Equal(t, q1.GlobalID(), ordered[1].GlobalID())
}

func TestOrderQuestionsWeak(t *testing.T) {
	ctx := context.Background()
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // Networks
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2) // Security
	pool := []*core.Question{q1, q2}

	c := core.New(bank, newPlanningTestRepo(t))
	sessionID, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", Mode: "normal", OrderStrategy: "random"})
	require.NoError(t, err)
	// Networks: 100% accuracy (low weakness weight); Security: never attempted
	// (default weight 0.5) -> Security should sort before Networks.
	_, err = c.SubmitAnswer(ctx, sessionID, q1.GlobalID(), "A", false, 0)
	require.NoError(t, err)

	ordered, err := orderQuestions(ctx, c, pool, "weak")
	require.NoError(t, err)
	require.Len(t, ordered, 2)
	assert.Equal(t, q2.GlobalID(), ordered[0].GlobalID(), "never-attempted topic should surface before a 100%%-accuracy one")
}

func TestOrderQuestionsUnknownStrategy(t *testing.T) {
	bank := newPlanningTestBank(t)
	c := core.New(bank, newPlanningTestRepo(t))
	_, err := orderQuestions(context.Background(), c, nil, "bogus")
	assert.Error(t, err)
}

func TestFilterByTopic(t *testing.T) {
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q1, q2}

	got := filterByTopic(pool, "Security")
	require.Len(t, got, 1)
	assert.Equal(t, q2.GlobalID(), got[0].GlobalID())
}

func TestReviewFiltered(t *testing.T) {
	ctx := context.Background()
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q1, q2}

	c := core.New(bank, newPlanningTestRepo(t))
	require.NoError(t, c.Repo.UpsertQuestionSRS(ctx, q1.GlobalID(), core.QuestionSRS{
		Box: 1, DueAt: time.Now().UTC().Add(-time.Minute), LastReviewedAt: time.Now().UTC(),
	}))

	got, err := reviewFiltered(ctx, c, pool)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, q1.GlobalID(), got[0].GlobalID())
}
