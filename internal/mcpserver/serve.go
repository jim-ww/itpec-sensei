package mcpserver

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

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
	server := mcp.NewServer(&mcp.Implementation{Name: "itpec-sensei", Version: "0.1.0"}, nil)
	registerTools(server, c, &imageBaseURL)

	if !*remote {
		return server.Run(ctx, &mcp.StdioTransport{})
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
		return httpServer.Close()
	}
	defer fwd.Close()
	imageBaseURL = fwd.URL().String()
	log.Printf("MCP server publicly reachable at %s", imageBaseURL)

	<-ctx.Done()
	fwd.Close()
	return httpServer.Close()
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

func registerTools(server *mcp.Server, c *core.Core, imageBaseURL *string) {
	// A single session is created lazily on first get_next_question call per server
	// process, since MCP tool calls within one conversation share this process.
	var sessionID int64
	var sessionStarted bool

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
		if !sessionStarted {
			id, err := c.StartSession(ctx, "fe", in.ExamID, orDefault(in.Mode, "normal"), "random", nil, nil)
			if err != nil {
				return nil, getNextQuestionOut{}, err
			}
			sessionID = id
			sessionStarted = true
		}
		q, err := c.GetNextQuestion(ctx, core.QuestionFilter{Topic: in.Topic, ExamID: in.ExamID, Mode: orDefault(in.Mode, "random")})
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		result := &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("sessionId=%d", sessionID)}},
		}
		imageURL := "/images/" + q.ImageRelPath()
		if *imageBaseURL != "" {
			imageURL = strings.TrimSuffix(*imageBaseURL, "/") + imageURL
		}
		return result, getNextQuestionOut{QuestionID: q.GlobalID(), ExamID: q.ExamID, ImageURL: imageURL}, nil
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
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
