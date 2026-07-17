package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerAnswerTools registers submit_answer and undo_last_answer.
func (t *toolCtx) registerAnswerTools(server *mcp.Server) {
	c := t.c

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_answer",
		Description: "Submit the user's stated answer letter for grading. Grading happens server-side; the AI is a conduit for the user's stated answer, not the judge of correctness — never invoke this with a letter the AI chose, guessed, or inferred instead of asking the user. If the user hasn't given an answer, or says they don't know, pass \"idk\" rather than guessing for them. Returns correct/correctAnswer/topic, no canned explanation — explain why yourself using your own knowledge of the topic.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in submitAnswerIn) (*mcp.CallToolResult, submitAnswerOut, error) {
		res, err := c.SubmitAnswer(ctx, in.SessionID, in.QuestionID, in.Answer, in.TimedOut, in.TimeTakenMs)
		if err != nil {
			return nil, submitAnswerOut{}, err
		}
		out := submitAnswerOut{Correct: res.Correct, CorrectAnswer: res.CorrectAnswer}
		if res.Explanation != nil {
			out.Topic = res.Explanation.Topic
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "undo_last_answer",
		Description: "Delete the most recently recorded attempt, undoing its grading. Use only when the user explicitly asks to undo/retract/redo their last answer — e.g. they mis-stated it or it was submitted by mistake.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in undoLastAnswerIn) (*mcp.CallToolResult, undoLastAnswerOut, error) {
		r, err := c.UndoLastAnswer(ctx, in.SessionID)
		if err != nil {
			return nil, undoLastAnswerOut{}, err
		}
		return nil, undoLastAnswerOut{
			QuestionID: r.QuestionID,
			ExamID:     r.ExamID,
			Topic:      r.Topic,
			Answer:     r.Answer,
			Correct:    r.Correct,
			AnsweredAt: r.AnsweredAt.Format(time.RFC3339),
		}, nil
	})
}
