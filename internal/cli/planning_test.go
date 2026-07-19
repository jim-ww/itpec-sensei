package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/internal/core"
	"github.com/jim-ww/itpec-sensei/internal/repository"
	"github.com/jim-ww/itpec-sensei/internal/repository/mocks"
)

// newPlanningTestBank builds a small fixture bank: exam "2020A_FE-A" with q1
// (topic "Networks") and q2 (topic "Security").
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

func TestOrderQuestionsSequential(t *testing.T) {
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q2, q1} // deliberately reversed

	c := core.New(bank, mocks.NewMockRepository(t))
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

	c := core.New(bank, mocks.NewMockRepository(t))
	ordered, err := orderQuestions(context.Background(), c, pool, "random")
	require.NoError(t, err)
	assert.ElementsMatch(t, pool, ordered)
}

func TestOrderQuestionsFailCount(t *testing.T) {
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q1, q2}

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		FailCounts(context.Background(), []string{q1.GlobalID(), q2.GlobalID()}).
		Return(map[string]int{q1.GlobalID(): 1, q2.GlobalID(): 5}, nil)

	c := core.New(bank, repo)
	ordered, err := orderQuestions(context.Background(), c, pool, "fail-count")
	require.NoError(t, err)
	require.Len(t, ordered, 2)
	// Higher fail count sorts first.
	assert.Equal(t, q2.GlobalID(), ordered[0].GlobalID())
	assert.Equal(t, q1.GlobalID(), ordered[1].GlobalID())
}

func TestOrderQuestionsWeak(t *testing.T) {
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1) // Networks
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2) // Security
	pool := []*core.Question{q1, q2}

	repo := mocks.NewMockRepository(t)
	// Networks: 100% accuracy (low weakness weight); Security: never attempted
	// (default weight 0.5) -> Security should sort before Networks.
	repo.EXPECT().
		ListAttempts(context.Background(), repository.AttemptFilter{}, repository.HistoryOrder(""), 0).
		Return([]repository.AttemptRow{
			{ID: 1, QuestionID: q1.GlobalID(), Correct: true},
		}, nil)

	c := core.New(bank, repo)
	ordered, err := orderQuestions(context.Background(), c, pool, "weak")
	require.NoError(t, err)
	require.Len(t, ordered, 2)
	assert.Equal(t, q2.GlobalID(), ordered[0].GlobalID(), "never-attempted topic should surface before a 100%%-accuracy one")
}

func TestOrderQuestionsUnknownStrategy(t *testing.T) {
	bank := newPlanningTestBank(t)
	c := core.New(bank, mocks.NewMockRepository(t))
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
	bank := newPlanningTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2)
	pool := []*core.Question{q1, q2}

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		DueQuestionIDs(context.Background(), mock.Anything).
		Return(map[string]bool{q1.GlobalID(): true}, nil)

	c := core.New(bank, repo)
	got, err := reviewFiltered(context.Background(), c, pool)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, q1.GlobalID(), got[0].GlobalID())
}
