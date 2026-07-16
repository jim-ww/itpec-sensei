package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.ngrok.com/ngrok/v2"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// Run implements `itpec-sensei serve [--remote]`.
func Run(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	remote := fs.Bool("remote", false, "expose over Streamable HTTP instead of stdio")
	addr := fs.String("addr", "127.0.0.1:8790", "local listen address for --remote")
	useNgrok := fs.Bool("ngrok", false, "also forward a public ngrok tunnel to the --remote server")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sess := &sessionState{}
	server := mcp.NewServer(&mcp.Implementation{Name: "itpec-sensei", Version: "0.1.0"}, nil)
	registerTools(server, c, sess)

	if !*remote {
		err := server.Run(ctx, &mcp.StdioTransport{})
		endMCPSession(c, sess)
		return err
	}

	// Question images are embedded directly in tool results (see
	// encodeQuestionImage), so the HTTP server here only needs to expose the
	// Streamable HTTP MCP endpoint itself, not a separate image route.
	//
	// The SDK's default DNS-rebinding protection rejects any request whose
	// Host header isn't localhost, since the server binds to a loopback
	// address. With --ngrok that's exactly what legitimate forwarded
	// requests look like (Host: <subdomain>.ngrok-free.app), so it has to be
	// disabled in that case — the tunnel itself is the intentional exposure.
	mux := http.NewServeMux()
	mux.Handle("/", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: *useNgrok}))

	httpServer := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		log.Printf("MCP server listening on http://%s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("local HTTP server stopped: %v", err)
		}
	}()

	if !*useNgrok {
		<-ctx.Done()
		endMCPSession(c, sess)
		return httpServer.Close()
	}

	// Forward a public ngrok endpoint to the local server, using the ngrok-go
	// SDK (NGROK_AUTHTOKEN env var) rather than shelling out to the ngrok binary.
	// NGROK_RESERVED_URL pins this to our reserved domain instead of a random one.
	var endpointOpts []ngrok.EndpointOption
	if reservedURL := os.Getenv("NGROK_RESERVED_URL"); reservedURL != "" {
		endpointOpts = append(endpointOpts, ngrok.WithURL(reservedURL))
	}
	fwd, err := ngrok.Forward(ctx, ngrok.WithUpstream("http://"+*addr), endpointOpts...)
	if err != nil {
		log.Printf("ngrok tunnel not started: %v (serving locally on %s only)", err, *addr)
		<-ctx.Done()
		endMCPSession(c, sess)
		return httpServer.Close()
	}
	defer fwd.Close()
	log.Printf("MCP server publicly reachable at %s", fwd.URL())

	<-ctx.Done()
	fwd.Close()
	endMCPSession(c, sess)
	return httpServer.Close()
}

// encodeQuestionImage decodes the question's embedded image, inverts it
// unless lightMode is set (dark mode is the default everywhere a question
// image is shown, matching the CLI's --dark default), and re-encodes it as
// PNG image content for direct embedding in a tool result. This is used for
// both stdio and --remote transports, so image delivery works identically
// regardless of how the MCP client connected — no separate HTTP image route
// or base URL needed.
func encodeQuestionImage(c *core.Core, q *core.Question, lightMode bool) (*mcp.ImageContent, error) {
	img, err := c.Bank.Image(q)
	if err != nil {
		return nil, fmt.Errorf("load image: %w", err)
	}
	if !lightMode {
		img = core.InvertImage(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	return &mcp.ImageContent{Data: buf.Bytes(), MIMEType: "image/png"}, nil
}

// sessionState tracks the single lazily-started progress-DB session shared by
// all tool calls in this server process (see get_next_question below).
type sessionState struct {
	id      int64
	started bool
}

// endMCPSession closes out the session row on graceful server shutdown, if
// one was ever started. Uses a fresh context since ctx is typically already
// done by the time this runs. There's no "interrupted" case to distinguish
// here (unlike CLI practice) — this only runs on graceful shutdown; a crash
// or kill -9 just leaves exit_reason NULL, same as before this existed.
func endMCPSession(c *core.Core, sess *sessionState) {
	if !sess.started {
		return
	}
	if err := c.EndSession(context.Background(), sess.id, "completed"); err != nil {
		log.Printf("end session: %v", err)
	}
}

type listTopicsOut struct {
	Topics []string `json:"topics"`
	Exams  []string `json:"exams"`
}

type getNextQuestionIn struct {
	Topic     string `json:"topic,omitempty" jsonschema:"filter by topic"`
	ExamID    string `json:"examId,omitempty" jsonschema:"filter by exam id"`
	Mode      string `json:"mode,omitempty" jsonschema:"random or review, default random"`
	LightMode bool   `json:"lightMode,omitempty" jsonschema:"if true, return the image with its original (light) colors instead of the default inverted (dark) version"`
}

type getNextQuestionOut struct {
	QuestionID string `json:"questionId" jsonschema:"opaque id, pass verbatim to submit_answer"`
	ExamID     string `json:"examId"`
}

type submitAnswerIn struct {
	SessionID   int64  `json:"sessionId" jsonschema:"session id from a prior get_next_question call in this conversation"`
	QuestionID  string `json:"questionId" jsonschema:"the opaque questionId returned by get_next_question"`
	Answer      string `json:"answer" jsonschema:"the answer letter stated by the user"`
	TimedOut    bool   `json:"timedOut,omitempty"`
	TimeTakenMs int    `json:"timeTakenMs,omitempty"`
}

type submitAnswerOut struct {
	Correct     bool   `json:"correct"`
	Topic       string `json:"topic,omitempty"`
	Explanation string `json:"explanation,omitempty"`
}

type getProgressSummaryIn struct {
	Scope  string `json:"scope,omitempty" jsonschema:"all | topic:<name> | exam:<id> | part:am | part:pm, default all"`
	Period string `json:"period,omitempty" jsonschema:"week | month | all, default all"`
}

type partStatOut struct {
	Part         string  `json:"part"` // "am" | "pm" | "other"
	Answered     int     `json:"answered"`
	Correct      int     `json:"correct"`
	Accuracy     float64 `json:"accuracy"`
	AvgTimeMs    float64 `json:"avgTimeMs,omitempty"`
	MedianTimeMs float64 `json:"medianTimeMs,omitempty"`
}

type topicStatOut struct {
	Topic    string  `json:"topic"`
	Answered int     `json:"answered"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

type examStatOut struct {
	ExamID   string  `json:"examId"`
	Answered int     `json:"answered"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

type getProgressSummaryOut struct {
	Answered    int            `json:"answered"`
	Streak      int            `json:"streak"`
	MaxStreak   int            `json:"maxStreak"`
	ReviewQueue int            `json:"reviewQueue"`
	PartStats   []partStatOut  `json:"partStats"`
	TopicStats  []topicStatOut `json:"topicStats,omitempty"`
	ExamStats   []examStatOut  `json:"examStats,omitempty"`
}

type getQuestionIn struct {
	ExamID       string `json:"examId" jsonschema:"exam id, e.g. 2025A_FE-A"`
	Number       int    `json:"number" jsonschema:"question number within the exam"`
	RevealAnswer bool   `json:"revealAnswer,omitempty" jsonschema:"if true, include the correct answer and explanation"`
	LightMode    bool   `json:"lightMode,omitempty" jsonschema:"if true, return the image with its original (light) colors instead of the default inverted (dark) version"`
}

type getQuestionOut struct {
	QuestionID   string           `json:"questionId"`
	ExamID       string           `json:"examId"`
	Topic        string           `json:"topic,omitempty"`
	Answer       json.RawMessage  `json:"answer,omitempty"`
	SimpleAnswer string           `json:"simpleAnswer,omitempty"`
	SubAnswers   []core.SubAnswer `json:"subAnswers,omitempty"`
	Explanation  string           `json:"explanation,omitempty"`
}

type getHistoryIn struct {
	Scope string `json:"scope,omitempty" jsonschema:"all | topic:<name> | exam:<id> | part:am | part:pm, default all"`
	Order string `json:"order,omitempty" jsonschema:"newest | oldest, default newest"`
	Limit int    `json:"limit,omitempty" jsonschema:"max attempts to return, default 20"`
}

type historyAttempt struct {
	QuestionID  string `json:"questionId"`
	ExamID      string `json:"examId"`
	Topic       string `json:"topic"`
	Answer      string `json:"answer"`
	Correct     bool   `json:"correct"`
	TimedOut    bool   `json:"timedOut"`
	TimeTakenMs int    `json:"timeTakenMs,omitempty"`
	AnsweredAt  string `json:"answeredAt"`
}

type getHistoryOut struct {
	Attempts []historyAttempt `json:"attempts"`
}

type getExamIn struct {
	ExamID string `json:"examId" jsonschema:"exam id, e.g. 2025A_FE-A"`
}

type getExamOut struct {
	ExamID          string  `json:"examId"`
	Name            string  `json:"name,omitempty"`
	Date            string  `json:"date,omitempty"`
	Part            string  `json:"part,omitempty"`
	DurationMinutes int     `json:"durationMinutes,omitempty"`
	TotalQuestions  int     `json:"totalQuestions"`
	Answered        int     `json:"answered"`
	Correct         int     `json:"correct"`
	Accuracy        float64 `json:"accuracy"`
}

type getSessionsIn struct {
	Scope string `json:"scope,omitempty" jsonschema:"all | exam:<id> | part:am | part:pm, default all (topic scope not supported)"`
	Order string `json:"order,omitempty" jsonschema:"newest | oldest, default newest"`
	Limit int    `json:"limit,omitempty" jsonschema:"max sessions to return, default 20"`
}

type sessionSummary struct {
	ID               int64  `json:"id"`
	StartedAt        string `json:"startedAt"`
	EndedAt          string `json:"endedAt,omitempty"`
	ExamID           string `json:"examId,omitempty"`
	Mode             string `json:"mode"`
	OrderStrategy    string `json:"orderStrategy"`
	TimeLimitSeconds int    `json:"timeLimitSeconds,omitempty"`
	ExitReason       string `json:"exitReason,omitempty"`
	Answered         int    `json:"answered"`
	Correct          int    `json:"correct"`
}

type getSessionsOut struct {
	Sessions []sessionSummary `json:"sessions"`
}

func registerTools(server *mcp.Server, c *core.Core, sess *sessionState) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_topics",
		Description: "List available topics and exam IDs, so the AI can offer filtering rather than guessing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, listTopicsOut, error) {
		topics, err := c.ListTopics(ctx)
		if err != nil {
			return nil, listTopicsOut{}, err
		}
		exams, err := c.ListExams(ctx)
		if err != nil {
			return nil, listTopicsOut{}, err
		}
		return nil, listTopicsOut{Topics: topics, Exams: exams}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_next_question",
		Description: "Return the next practice question (embedded image + id + topic), filtered by topic/exam/mode. Never includes the answer.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getNextQuestionIn) (*mcp.CallToolResult, getNextQuestionOut, error) {
		if !sess.started {
			id, err := c.StartSession(ctx, "fe", in.ExamID, orDefault(in.Mode, "normal"), "random", nil, nil)
			if err != nil {
				return nil, getNextQuestionOut{}, err
			}
			sess.id = id
			sess.started = true
		}
		q, err := c.GetNextQuestion(ctx, core.QuestionFilter{Topic: in.Topic, ExamID: in.ExamID, Mode: orDefault(in.Mode, "random")})
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		imgContent, err := encodeQuestionImage(c, q, in.LightMode)
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		result := &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("sessionId=%d", sess.id)},
				imgContent,
			},
		}
		return result, getNextQuestionOut{QuestionID: q.GlobalID(), ExamID: q.ExamID}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_question",
		Description: "Look up one specific question by exam id + question number, e.g. question 34 of 2025A_FE-A. Returns the embedded image. Set revealAnswer=true to also return the correct answer and explanation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getQuestionIn) (*mcp.CallToolResult, getQuestionOut, error) {
		q, err := c.GetQuestion(ctx, in.ExamID, in.Number, in.RevealAnswer)
		if err != nil {
			return nil, getQuestionOut{}, err
		}
		imgContent, err := encodeQuestionImage(c, q, in.LightMode)
		if err != nil {
			return nil, getQuestionOut{}, err
		}
		result := &mcp.CallToolResult{Content: []mcp.Content{imgContent}}
		out := getQuestionOut{QuestionID: q.GlobalID(), ExamID: q.ExamID}
		if q.Explanation != nil {
			out.Topic = q.Explanation.Topic
			out.Explanation = q.Explanation.Explanation
		}
		out.Answer = q.Answer
		out.SimpleAnswer = q.SimpleAnswer
		out.SubAnswers = q.SubAnswers
		return result, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_answer",
		Description: "Submit the user's stated answer letter for grading. Grading happens server-side; the AI is a conduit for the user's stated answer, not the judge of correctness.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in submitAnswerIn) (*mcp.CallToolResult, submitAnswerOut, error) {
		res, err := c.SubmitAnswer(ctx, in.SessionID, in.QuestionID, in.Answer, in.TimedOut, in.TimeTakenMs)
		if err != nil {
			return nil, submitAnswerOut{}, err
		}
		out := submitAnswerOut{Correct: res.Correct}
		if res.Explanation != nil {
			out.Topic = res.Explanation.Topic
			out.Explanation = res.Explanation.Explanation
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_progress_summary",
		Description: "Return accuracy/streak/review-queue plus per-part (AM/PM), per-topic, and per-exam breakdowns, so the AI can meaningfully reference progress in conversation. AM and PM are never blended into one accuracy/timing number since they test very different material.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getProgressSummaryIn) (*mcp.CallToolResult, getProgressSummaryOut, error) {
		scope := core.Scope(orDefault(in.Scope, "all"))
		period := core.Period(orDefault(in.Period, "all"))
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
		for _, t := range topicStats {
			out.TopicStats = append(out.TopicStats, topicStatOut{
				Topic: t.Topic, Answered: t.Answered, Correct: t.Correct, Accuracy: t.Accuracy,
			})
		}
		for _, e := range examStats {
			out.ExamStats = append(out.ExamStats, examStatOut{
				ExamID: e.ExamID, Answered: e.Answered, Correct: e.Correct, Accuracy: e.Accuracy,
			})
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_history",
		Description: "List past attempts (newest first), so the AI can reference specific questions the user already answered.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getHistoryIn) (*mcp.CallToolResult, getHistoryOut, error) {
		scope := core.Scope(orDefault(in.Scope, "all"))
		order := core.HistoryOrder(orDefault(in.Order, "newest"))
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
		scope := core.Scope(orDefault(in.Scope, "all"))
		order := core.HistoryOrder(orDefault(in.Order, "newest"))
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		records, err := c.GetSessions(ctx, scope, order, limit)
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
