package mcpserver

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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

	var imageBaseURL string
	sess := &sessionState{}
	server := mcp.NewServer(&mcp.Implementation{Name: "itpec-sensei", Version: "0.1.0"}, nil)
	registerTools(server, c, &imageBaseURL, sess)

	if !*remote {
		err := server.Run(ctx, &mcp.StdioTransport{})
		endMCPSession(c, sess)
		return err
	}

	imagesFS, err := c.Bank.ImagesFS()
	if err != nil {
		return fmt.Errorf("serve: images fs: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServerFS(imagesFS)))
	mux.Handle("/", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))

	httpServer := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		log.Printf("MCP server listening on http://%s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("local HTTP server stopped: %v", err)
		}
	}()

	if !*useNgrok {
		imageBaseURL = "http://" + *addr
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
		imageBaseURL = "http://" + *addr
		<-ctx.Done()
		endMCPSession(c, sess)
		return httpServer.Close()
	}
	defer fwd.Close()
	imageBaseURL = fwd.URL().String()
	log.Printf("MCP server publicly reachable at %s", imageBaseURL)

	<-ctx.Done()
	fwd.Close()
	endMCPSession(c, sess)
	return httpServer.Close()
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
	Topic  string `json:"topic,omitempty" jsonschema:"filter by topic"`
	ExamID string `json:"examId,omitempty" jsonschema:"filter by exam id"`
	Mode   string `json:"mode,omitempty" jsonschema:"random or review, default random"`
}

type getNextQuestionOut struct {
	QuestionID string `json:"questionId" jsonschema:"opaque id, pass verbatim to submit_answer"`
	ExamID     string `json:"examId"`
	ImageURL   string `json:"imageUrl"`
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

type getProgressSummaryOut struct {
	Answered     int     `json:"answered"`
	Correct      int     `json:"correct"`
	Accuracy     float64 `json:"accuracy"`
	Streak       int     `json:"streak"`
	ReviewQueue  int     `json:"reviewQueue"`
	AvgTimeMs    float64 `json:"avgTimeMs,omitempty"`
	MedianTimeMs float64 `json:"medianTimeMs,omitempty"`
}

type getQuestionIn struct {
	ExamID       string `json:"examId" jsonschema:"exam id, e.g. 2025A_FE-A"`
	Number       int    `json:"number" jsonschema:"question number within the exam"`
	RevealAnswer bool   `json:"revealAnswer,omitempty" jsonschema:"if true, include the correct answer and explanation"`
}

type getQuestionOut struct {
	QuestionID   string           `json:"questionId"`
	ExamID       string           `json:"examId"`
	ImageURL     string           `json:"imageUrl"`
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

func registerTools(server *mcp.Server, c *core.Core, imageBaseURL *string, sess *sessionState) {
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
		Description: "Return the next practice question (image + id + topic), filtered by topic/exam/mode. Never includes the answer.",
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
		result := &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("sessionId=%d", sess.id)}},
		}
		imageURL := "/images/" + q.ImageRelPath()
		if *imageBaseURL != "" {
			imageURL = strings.TrimSuffix(*imageBaseURL, "/") + imageURL
		}
		return result, getNextQuestionOut{QuestionID: q.GlobalID(), ExamID: q.ExamID, ImageURL: imageURL}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_question",
		Description: "Look up one specific question by exam id + question number, e.g. question 34 of 2025A_FE-A. Set revealAnswer=true to also return the correct answer and explanation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getQuestionIn) (*mcp.CallToolResult, getQuestionOut, error) {
		q, err := c.GetQuestion(ctx, in.ExamID, in.Number, in.RevealAnswer)
		if err != nil {
			return nil, getQuestionOut{}, err
		}
		imageURL := "/images/" + q.ImageRelPath()
		if *imageBaseURL != "" {
			imageURL = strings.TrimSuffix(*imageBaseURL, "/") + imageURL
		}
		out := getQuestionOut{QuestionID: q.GlobalID(), ExamID: q.ExamID, ImageURL: imageURL}
		if q.Explanation != nil {
			out.Topic = q.Explanation.Topic
			out.Explanation = q.Explanation.Explanation
		}
		out.Answer = q.Answer
		out.SimpleAnswer = q.SimpleAnswer
		out.SubAnswers = q.SubAnswers
		return nil, out, nil
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
		Description: "Return accuracy/streak/review-queue so the AI can meaningfully reference progress in conversation.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getProgressSummaryIn) (*mcp.CallToolResult, getProgressSummaryOut, error) {
		scope := core.Scope(orDefault(in.Scope, "all"))
		period := core.Period(orDefault(in.Period, "all"))
		s, err := c.GetProgressSummary(ctx, scope, period)
		if err != nil {
			return nil, getProgressSummaryOut{}, err
		}
		return nil, getProgressSummaryOut{
			Answered:     s.Answered,
			Correct:      s.Correct,
			Accuracy:     s.Accuracy,
			Streak:       s.Streak,
			ReviewQueue:  s.ReviewQueue,
			AvgTimeMs:    s.AvgTimeMs,
			MedianTimeMs: s.MedianTimeMs,
		}, nil
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
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
