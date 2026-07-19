package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jim-ww/itpec-sensei/internal/repository"
)

// SubmitAnswer grades answer against the embedded correct answer for questionID
// (a Question.GlobalID(), as returned by GetNextQuestion), records the attempt,
// and returns correctness + explanation.
func (c *Core) SubmitAnswer(ctx context.Context, sessionID int64, questionID string, answer string, timedOut bool, timeTakenMs int) (*AnswerResult, error) {
	q := c.Bank.Question(questionID)
	if q == nil {
		return nil, fmt.Errorf("unknown question id %q", questionID)
	}

	answer = normalizeIdk(answer)
	correct := gradeAnswer(q, answer)

	now := time.Now().UTC()
	if err := c.Repo.InsertAttempt(ctx, sessionID, questionID, answer, correct, timedOut, timeTakenMs, now); err != nil {
		return nil, fmt.Errorf("record attempt: %w", err)
	}
	if err := c.updateSRS(ctx, questionID, correct, now); err != nil {
		return nil, fmt.Errorf("update srs: %w", err)
	}

	return &AnswerResult{Correct: correct, CorrectAnswer: correctAnswerLabel(q), Explanation: q.Explanation}, nil
}

// correctAnswerLabel returns the answer letter callers should be told is
// correct, mirroring gradeAnswer's notion of "the" answer for multi-part
// questions (the first sub-answer's expected letter).
func correctAnswerLabel(q *Question) string {
	if q.SimpleAnswer != "" {
		return q.SimpleAnswer
	}
	if len(q.SubAnswers) > 0 {
		return q.SubAnswers[0].Answer
	}
	return ""
}

// UndoLastAnswer deletes the most recently recorded attempt and returns what
// it deleted, so a mis-stated or accidentally submitted answer can be
// retracted. If sessionID is nonzero, only that session's attempts are
// considered; otherwise the single most recent attempt across all sessions
// is undone.
func (c *Core) UndoLastAnswer(ctx context.Context, sessionID int64) (*AttemptRecord, error) {
	rows, err := c.Repo.ListAttempts(ctx, repository.AttemptFilter{SessionID: sessionID}, HistoryNewestFirst, 1)
	if err != nil {
		return nil, fmt.Errorf("find last attempt: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no attempts to undo")
	}
	last := rows[0]

	r := &AttemptRecord{
		QuestionID:  last.QuestionID,
		Answer:      last.Answer,
		Correct:     last.Correct,
		TimedOut:    last.TimedOut,
		TimeTakenMs: last.TimeTakenMs,
		AnsweredAt:  last.AnsweredAt,
	}
	if q := c.Bank.Question(r.QuestionID); q != nil {
		r.ExamID = q.ExamID
		r.Topic = q.Topic()
	}

	if err := c.Repo.DeleteAttempt(ctx, last.ID); err != nil {
		return nil, err
	}
	return r, nil
}

// normalizeIdk maps the two "I don't know" sentinels callers are asked to
// send — "?" (CLI keypress) and "idk" (both CLI and, per the MCP tool's
// documented contract, what the AI must pass when the user doesn't know,
// rather than relaying the user's raw wording, which is too open-ended to
// pattern-match reliably) — onto a single canonical value, so it's recorded
// consistently in the attempts log (distinguishable from a wrong guess) and
// always grades as incorrect.
func normalizeIdk(answer string) string {
	if answer == "?" || strings.EqualFold(answer, "idk") {
		return "idk"
	}
	return answer
}

func gradeAnswer(q *Question, answer string) bool {
	// Simple exams only submit a single letter; for multi-part questions,
	// treat the submission as matching the first sub-answer's expected letter
	// (see correctAnswerLabel).
	want := correctAnswerLabel(q)
	return want != "" && strings.EqualFold(want, strings.TrimSpace(answer))
}
