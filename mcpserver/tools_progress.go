package mcpserver

import (
	"cmp"
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jim-ww/itpec-sensei/core"
)

// registerProgressTools registers get_progress_summary, get_history,
// get_sessions, and get_exam.
func (t *toolCtx) registerProgressTools(server *mcp.Server) {
	c := t.c

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_progress_summary",
		Description: "Return accuracy/streak/review-queue plus per-part (AM/PM), per-topic, and per-exam breakdowns, so the AI can meaningfully reference progress in conversation. AM and PM are never blended into one accuracy/timing number since they test very different material.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getProgressSummaryIn) (*mcp.CallToolResult, getProgressSummaryOut, error) {
		scope := core.Scope(cmp.Or(in.Scope, "all"))
		period := core.Period(cmp.Or(in.Period, "all"))
		s, err := c.GetProgressSummary(ctx, scope, period)
		if err != nil {
			return nil, getProgressSummaryOut{}, err
		}
		topicStats, err := c.GetTopicStats(ctx, scope)
		if err != nil {
			return nil, getProgressSummaryOut{}, err
		}
		examStats, err := c.GetExamStats(ctx, scope)
		if err != nil {
			return nil, getProgressSummaryOut{}, err
		}
		tagStats, err := c.GetTagStats(ctx, scope)
		if err != nil {
			return nil, getProgressSummaryOut{}, err
		}
		weakestTagsLimit := in.WeakestTags
		if weakestTagsLimit == 0 {
			weakestTagsLimit = 10
		} else if weakestTagsLimit < 0 {
			weakestTagsLimit = 0 // core.WeakestTags treats <=0 as unlimited
		}

		out := getProgressSummaryOut{
			Answered:    s.Answered,
			Streak:      s.Streak,
			MaxStreak:   s.MaxStreak,
			ReviewQueue: s.ReviewQueue,
		}
		for _, p := range s.PartStats {
			out.PartStats = append(out.PartStats, partStatOut{
				Part: p.Part, Answered: p.Answered, Correct: p.Correct, Accuracy: p.Accuracy,
				AvgTimeMs: p.AvgTimeMs, MedianTimeMs: p.MedianTimeMs,
			})
		}
		for _, tp := range topicStats {
			out.TopicStats = append(out.TopicStats, topicStatOut{
				Topic: tp.Topic, Answered: tp.Answered, Correct: tp.Correct, Accuracy: tp.Accuracy,
			})
		}
		for _, e := range examStats {
			out.ExamStats = append(out.ExamStats, examStatOut{
				ExamID: e.ExamID, Answered: e.Answered, Correct: e.Correct, Accuracy: e.Accuracy,
			})
		}
		for _, tg := range core.WeakestTags(tagStats, weakestTagsLimit, core.MinTagAttempts) {
			out.TagStats = append(out.TagStats, tagStatOut{
				Tag: tg.Tag, Answered: tg.Answered, Correct: tg.Correct, Accuracy: tg.Accuracy,
			})
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_history",
		Description: "List past attempts (newest first), so the AI can reference specific questions the user already answered.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getHistoryIn) (*mcp.CallToolResult, getHistoryOut, error) {
		scope := core.Scope(cmp.Or(in.Scope, "all"))
		order := core.HistoryOrder(cmp.Or(in.Order, "newest"))
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		records, err := c.GetHistory(ctx, scope, order, limit)
		if err != nil {
			return nil, getHistoryOut{}, err
		}
		out := getHistoryOut{Attempts: make([]historyAttempt, len(records))}
		for i, r := range records {
			out.Attempts[i] = historyAttempt{
				QuestionID:  r.QuestionID,
				ExamID:      r.ExamID,
				Topic:       r.Topic,
				Answer:      r.Answer,
				Correct:     r.Correct,
				TimedOut:    r.TimedOut,
				TimeTakenMs: r.TimeTakenMs,
				AnsweredAt:  r.AnsweredAt.Format(time.RFC3339),
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_sessions",
		Description: "List past practice sessions (newest first) with their score and completion status, so the AI can reference how a study run went (e.g. a timed mock exam that was completed vs interrupted), not just individual answers.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getSessionsIn) (*mcp.CallToolResult, getSessionsOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		var records []core.SessionRecord
		var err error
		if in.IncompleteOnly {
			records, err = c.IncompleteSessions(ctx, limit)
		} else {
			scope := core.Scope(cmp.Or(in.Scope, "all"))
			order := core.HistoryOrder(cmp.Or(in.Order, "newest"))
			records, err = c.GetSessions(ctx, scope, order, limit)
		}
		if err != nil {
			return nil, getSessionsOut{}, err
		}
		out := getSessionsOut{Sessions: make([]sessionSummary, len(records))}
		for i, r := range records {
			s := sessionSummary{
				ID:            r.ID,
				StartedAt:     r.StartedAt.Format(time.RFC3339),
				ExamID:        r.ExamID,
				Mode:          r.Mode,
				OrderStrategy: r.OrderStrategy,
				ExitReason:    r.ExitReason,
				Answered:      r.Answered,
				Correct:       r.Correct,
			}
			if r.EndedAt != nil {
				s.EndedAt = r.EndedAt.Format(time.RFC3339)
			}
			if r.TimeLimitSeconds != nil {
				s.TimeLimitSeconds = *r.TimeLimitSeconds
			}
			out.Sessions[i] = s
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_exam",
		Description: "Return readable metadata (name, date, duration, question count) for one exam plus the user's own progress on it.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getExamIn) (*mcp.CallToolResult, getExamOut, error) {
		detail, err := c.GetExam(ctx, in.ExamID)
		if err != nil {
			return nil, getExamOut{}, err
		}
		return nil, getExamOut{
			ExamID:          detail.ExamID,
			Name:            detail.Name,
			Date:            detail.Date,
			Part:            detail.Part,
			DurationMinutes: detail.DurationMinutes,
			TotalQuestions:  detail.TotalQuestions,
			Answered:        detail.Answered,
			Correct:         detail.Correct,
			Accuracy:        detail.Accuracy,
		}, nil
	})
}
