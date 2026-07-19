package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/internal/repository"
	"github.com/jim-ww/itpec-sensei/internal/repository/mocks"
)

func TestNormalizeIdk(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"question mark sentinel", "?", "idk"},
		{"lowercase idk", "idk", "idk"},
		{"uppercase IDK", "IDK", "idk"},
		{"mixed case IdK", "IdK", "idk"},
		{"regular answer untouched", "A", "A"},
		{"empty string untouched", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeIdk(tt.in))
		})
	}
}

func TestGradeAnswer(t *testing.T) {
	simple := &Question{SimpleAnswer: "A"}
	multiPart := &Question{SubAnswers: []SubAnswer{{SQ: 1, Answer: "c"}, {SQ: 2, Answer: "d"}}}
	noAnswer := &Question{}

	tests := []struct {
		name   string
		q      *Question
		answer string
		want   bool
	}{
		{"exact match", simple, "A", true},
		{"case insensitive match", simple, "a", true},
		{"whitespace trimmed", simple, "  A  ", true},
		{"wrong letter", simple, "B", false},
		{"idk sentinel never matches", simple, "idk", false},
		{"multi-part matches first sub-answer", multiPart, "c", true},
		{"multi-part case insensitive", multiPart, "C", true},
		{"multi-part wrong letter", multiPart, "d", false},
		{"question with no answer never grades correct", noAnswer, "", false},
		{"question with no answer never grades correct nonempty", noAnswer, "A", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, gradeAnswer(tt.q, tt.answer))
		})
	}
}

func TestCorrectAnswerLabel(t *testing.T) {
	tests := []struct {
		name string
		q    *Question
		want string
	}{
		{"simple answer wins", &Question{SimpleAnswer: "A", SubAnswers: []SubAnswer{{Answer: "z"}}}, "A"},
		{"falls back to first sub-answer", &Question{SubAnswers: []SubAnswer{{SQ: 1, Answer: "c"}, {SQ: 2, Answer: "d"}}}, "c"},
		{"neither set returns empty", &Question{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, correctAnswerLabel(tt.q))
		})
	}
}

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
			repo := mocks.NewMockRepository(t)
			var recordedAnswer string
			var recordedCorrect bool
			repo.EXPECT().
				InsertAttempt(context.Background(), int64(1), q.GlobalID(), mock.Anything, mock.Anything, false, 0, mock.Anything).
				RunAndReturn(func(ctx context.Context, sessionID int64, questionID, answer string, correct, timedOut bool, timeTakenMs int, answeredAt time.Time) error {
					recordedAnswer = answer
					recordedCorrect = correct
					return nil
				})
			repo.EXPECT().GetQuestionSRS(context.Background(), q.GlobalID()).Return(repository.QuestionSRS{}, false, nil)
			repo.EXPECT().UpsertQuestionSRS(context.Background(), q.GlobalID(), mock.Anything).Return(nil)

			c := New(bank, repo)
			res, err := c.SubmitAnswer(context.Background(), 1, q.GlobalID(), tt.answer, false, 0)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCorrect, res.Correct)
			assert.Equal(t, "A", res.CorrectAnswer)
			assert.Equal(t, tt.wantRecorded, recordedAnswer)
			assert.Equal(t, tt.wantCorrect, recordedCorrect)
		})
	}
}

func TestSubmitAnswerUnknownQuestion(t *testing.T) {
	bank := newTestBank(t)
	repo := mocks.NewMockRepository(t)
	c := New(bank, repo)

	_, err := c.SubmitAnswer(context.Background(), 1, "no-such-exam#99", "A", false, 0)
	assert.Error(t, err)
}

func TestUndoLastAnswer(t *testing.T) {
	bank := newTestBank(t)
	q := bank.QuestionByExamAndNumber("2020A_FE-A", 1)
	answeredAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("undoes the most recent attempt", func(t *testing.T) {
		repo := mocks.NewMockRepository(t)
		repo.EXPECT().
			ListAttempts(context.Background(), repository.AttemptFilter{SessionID: 5}, HistoryNewestFirst, 1).
			Return([]repository.AttemptRow{{ID: 42, QuestionID: q.GlobalID(), Answer: "A", Correct: true, AnsweredAt: answeredAt}}, nil)
		repo.EXPECT().DeleteAttempt(context.Background(), int64(42)).Return(nil)

		c := New(bank, repo)
		r, err := c.UndoLastAnswer(context.Background(), 5)
		require.NoError(t, err)
		assert.Equal(t, q.GlobalID(), r.QuestionID)
		assert.Equal(t, "2020A_FE-A", r.ExamID)
		assert.Equal(t, "Networks", r.Topic)
		assert.True(t, r.Correct)
	})

	t.Run("errors when there is nothing to undo", func(t *testing.T) {
		repo := mocks.NewMockRepository(t)
		repo.EXPECT().
			ListAttempts(context.Background(), repository.AttemptFilter{SessionID: 0}, HistoryNewestFirst, 1).
			Return(nil, nil)

		c := New(bank, repo)
		_, err := c.UndoLastAnswer(context.Background(), 0)
		assert.Error(t, err)
	})
}
