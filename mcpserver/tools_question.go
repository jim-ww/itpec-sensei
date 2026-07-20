package mcpserver

import (
	"cmp"
	"context"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jim-ww/itpec-sensei/core"
)

// registerQuestionTools registers list_topics, get_next_question,
// get_question, and open_question_image.
func (t *toolCtx) registerQuestionTools(server *mcp.Server) {
	c, sess := t.c, t.sess

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_topics",
		Description: "List available topics (grouped by AM/PM exam session, since AM and PM topics are unrelated) and exam IDs, so the AI can offer filtering rather than guessing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, listTopicsOut, error) {
		am, pm, other, err := c.ListTopicsByPart(ctx)
		if err != nil {
			return nil, listTopicsOut{}, err
		}
		exams, err := c.ListExams(ctx)
		if err != nil {
			return nil, listTopicsOut{}, err
		}
		return nil, listTopicsOut{TopicsAM: am, TopicsPM: pm, TopicsOther: other, Exams: exams}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_next_question",
		Description: "Return the next practice question, filtered by topic/exam/mode. The question text, diagrams, and answer choices are ONLY in the image at imageUrl — nothing here contains the question itself. You must fetch/view that image yourself to know what's actually being asked before answering or discussing it. When you relay the question to the user, transcribe it as-is — don't reword, summarize, or paraphrase it unless the user explicitly asks you to. If the question has visuals that can't be reliably described in text, call open_question_image to show it to the user directly. Never includes the answer. The tool result also embeds the image directly (always in original/light colors, regardless of imageUrl's dark default) as a fallback for clients that can't fetch imageUrl.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getNextQuestionIn) (*mcp.CallToolResult, getNextQuestionOut, error) {
		if in.ContinueSessionID != 0 && in.RepeatSessionID != 0 {
			return nil, getNextQuestionOut{}, fmt.Errorf("continueSessionId and repeatSessionId are mutually exclusive")
		}
		topic, examID := in.Topic, in.ExamID
		if !sess.started {
			switch {
			case in.ContinueSessionID != 0:
				incomplete, err := c.IncompleteSessions(ctx, 0)
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				found := false
				for _, s := range incomplete {
					if s.ID == in.ContinueSessionID {
						found = true
						break
					}
				}
				if !found {
					return nil, getNextQuestionOut{}, fmt.Errorf("session %d is not resumable (already completed, or doesn't exist)", in.ContinueSessionID)
				}
				params, err := c.GetSessionParams(ctx, in.ContinueSessionID)
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				if topic == "" {
					topic = params.Topic
				}
				if examID == "" {
					examID = params.ExamID
				}
				sess.id = in.ContinueSessionID
				sess.started = true
			case in.RepeatSessionID != 0:
				params, err := c.GetSessionParams(ctx, in.RepeatSessionID)
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				if topic == "" {
					topic = params.Topic
				}
				if examID == "" {
					examID = params.ExamID
				}
				id, err := c.StartSession(ctx, core.SessionParams{
					ExamType: "fe", ExamID: examID, Topic: topic,
					Mode: cmp.Or(in.Mode, params.Mode), OrderStrategy: "random",
				})
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				sess.id = id
				sess.started = true
			default:
				id, err := c.StartSession(ctx, core.SessionParams{
					ExamType: "fe", ExamID: examID, Topic: topic,
					Mode: cmp.Or(in.Mode, "normal"), OrderStrategy: "random",
				})
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				sess.id = id
				sess.started = true
			}
		}
		mode := cmp.Or(in.Mode, "random")
		var excludeIDs []string
		if strings.EqualFold(mode, "sequential") {
			answered, err := c.AnsweredQuestionIDs(ctx, sess.id)
			if err != nil {
				return nil, getNextQuestionOut{}, err
			}
			for id := range answered {
				excludeIDs = append(excludeIDs, id)
			}
		}
		q, err := c.GetNextQuestion(ctx, core.QuestionFilter{Topic: topic, ExamID: examID, Mode: mode, ExcludeIDs: excludeIDs})
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		imgContent, err := t.embedImageFor(q)
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		out := getNextQuestionOut{
			SessionID: sess.id, QuestionID: q.GlobalID(), ExamID: q.ExamID, Number: q.ID,
			ImageURL: t.imageURLFor(q, in.LightMode), ImageMode: t.imageModeFor(in.LightMode),
		}
		// structuredContent isn't reliably surfaced to the model by every MCP
		// client, so also spell out sessionId/questionId as plain text —
		// submit_answer needs both verbatim and they must not get lost.
		summary := fmt.Sprintf("sessionId=%d questionId=%s examId=%s number=%d imageMode=%s",
			out.SessionID, out.QuestionID, out.ExamID, out.Number, out.ImageMode)
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: summary}, imgContent}}
		return result, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_question",
		Description: "Look up one specific question by exam id + question number, e.g. question 34 of 2025A_FE-A — the same examId+number get_next_question returns, so you can look a question back up later (re-fetch, toggle lightMode, or reveal the answer). The question text, diagrams, and answer choices are ONLY in the image at imageUrl — nothing here contains the question itself; fetch/view that image yourself before answering or discussing it. When you relay the question to the user, transcribe it as-is — don't reword, summarize, or paraphrase it unless the user explicitly asks you to. If the question has visuals that can't be reliably described in text, call open_question_image to show it to the user directly. Set revealAnswer=true to also return the correct answer (no canned explanation text — explain it yourself) — in which case the question isn't submittable, there's no sessionId, since a revealed answer isn't meant to be graded. The tool result also embeds the image directly (always in original/light colors, regardless of imageUrl's dark default) as a fallback for clients that can't fetch imageUrl.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getQuestionIn) (*mcp.CallToolResult, getQuestionOut, error) {
		q, err := c.GetQuestion(ctx, in.ExamID, in.Number, in.RevealAnswer)
		if err != nil {
			return nil, getQuestionOut{}, err
		}
		out := getQuestionOut{
			QuestionID: q.GlobalID(), ExamID: q.ExamID, Number: q.ID,
			ImageURL: t.imageURLFor(q, in.LightMode), ImageMode: t.imageModeFor(in.LightMode),
		}
		if !in.RevealAnswer {
			if !sess.started {
				id, err := c.StartSession(ctx, core.SessionParams{ExamType: "fe", ExamID: in.ExamID, Mode: "lookup", OrderStrategy: "direct"})
				if err != nil {
					return nil, getQuestionOut{}, err
				}
				sess.id = id
				sess.started = true
			}
			out.SessionID = sess.id
		}
		if q.Explanation != nil {
			out.Topic = q.Explanation.Topic
		}
		out.Answer = q.Answer
		out.SimpleAnswer = q.SimpleAnswer
		out.SubAnswers = q.SubAnswers
		imgContent, err := t.embedImageFor(q)
		if err != nil {
			return nil, getQuestionOut{}, err
		}
		// structuredContent isn't reliably surfaced to the model by every MCP
		// client, so also spell out sessionId/questionId as plain text —
		// submit_answer needs both verbatim and they must not get lost.
		summary := fmt.Sprintf("questionId=%s examId=%s number=%d imageMode=%s", out.QuestionID, out.ExamID, out.Number, out.ImageMode)
		if out.SessionID != 0 {
			summary = fmt.Sprintf("sessionId=%d %s", out.SessionID, summary)
		}
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: summary}, imgContent}}
		return result, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "open_question_image",
		Description: "Display this question's image, with its answer options, to the user directly, exactly as it is. Call this when the question has visuals — diagrams, charts, tables, code — that can't be reliably described in text alone.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in openQuestionImageIn) (*mcp.CallToolResult, openQuestionImageOut, error) {
		q := c.Bank.QuestionByExamAndNumber(in.ExamID, in.Number)
		if q == nil {
			return nil, openQuestionImageOut{}, fmt.Errorf("question %s#%d not found", in.ExamID, in.Number)
		}
		img, err := c.Bank.Image(q)
		if err != nil {
			return nil, openQuestionImageOut{}, err
		}
		if !in.LightMode {
			img = core.InvertImage(img)
		}
		tmp, err := os.CreateTemp("", "itpec-sensei-*.png")
		if err != nil {
			return nil, openQuestionImageOut{}, err
		}
		defer tmp.Close()
		if err := png.Encode(tmp, img); err != nil {
			return nil, openQuestionImageOut{}, err
		}

		killPreviousViewer(sess)
		if err := exec.Command(t.imageViewerCmd, tmp.Name()).Start(); err != nil {
			return nil, openQuestionImageOut{}, fmt.Errorf("%s %s: %w", t.imageViewerCmd, tmp.Name(), err)
		}
		sess.lastExternalImage = tmp.Name()

		return nil, openQuestionImageOut{Opened: true, Viewer: t.imageViewerCmd, ImageMode: t.imageModeFor(in.LightMode)}, nil
	})
}
