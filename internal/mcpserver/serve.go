package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
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
	imageViewer := fs.String("image-viewer", "xdg-open", "local command the open_question_image MCP tool uses to open images on the machine running this server")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sess := &sessionState{}
	server := mcp.NewServer(&mcp.Implementation{Name: "itpec-sensei", Version: "0.1.0"}, nil)

	imgFS, err := c.Bank.ImagesFS()
	if err != nil {
		return fmt.Errorf("images fs: %w", err)
	}

	if !*remote {
		// Question images are always served as a URL too (get_next_question
		// and get_question also embed them as base64 tool content, but some
		// MCP clients, e.g. Claude web, don't render embedded image blocks to
		// the user even though the model can read them — a plain URL in a
		// markdown image link does). Over stdio there's no existing HTTP
		// server to point at, so spin up a local-only one (OS-assigned port)
		// just for images.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("listen for image server: %w", err)
		}
		defer ln.Close()
		imgMux := http.NewServeMux()
		imgMux.Handle("/images/", http.StripPrefix("/images/", imageHandler(imgFS)))
		go func() {
			if err := http.Serve(ln, imgMux); err != nil && err != http.ErrServerClosed {
				log.Printf("local image server stopped: %v", err)
			}
		}()

		var baseURL atomic.Pointer[string]
		local := "http://" + ln.Addr().String()
		baseURL.Store(&local)
		registerTools(server, c, sess, &baseURL, *imageViewer)

		err = server.Run(ctx, &mcp.StdioTransport{})
		endMCPSession(c, sess)
		return err
	}

	var baseURL atomic.Pointer[string]
	local := "http://" + *addr
	baseURL.Store(&local)
	registerTools(server, c, sess, &baseURL, *imageViewer)

	// The SDK's default DNS-rebinding protection rejects any request whose
	// Host header isn't localhost, since the server binds to a loopback
	// address. With --ngrok that's exactly what legitimate forwarded
	// requests look like (Host: <subdomain>.ngrok-free.app), so it has to be
	// disabled in that case — the tunnel itself is the intentional exposure.
	mux := http.NewServeMux()
	mux.Handle("/", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: *useNgrok}))
	mux.Handle("/images/", http.StripPrefix("/images/", imageHandler(imgFS)))

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
	publicURL := fwd.URL().String()
	baseURL.Store(&publicURL)
	log.Printf("MCP server publicly reachable at %s", publicURL)

	<-ctx.Done()
	fwd.Close()
	endMCPSession(c, sess)
	return httpServer.Close()
}

// imageHandler serves question images from imgFS (the bank's embedded
// "images" subtree) with colors inverted by default (dark mode), matching
// the inline embedding's default. Pass ?light=1 to get the original colors.
func imageHandler(imgFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		f, err := imgFS.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			http.Error(w, fmt.Sprintf("decode image: %v", err), http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("light") != "1" {
			img = core.InvertImage(img)
		}
		w.Header().Set("Content-Type", "image/png")
		if err := png.Encode(w, img); err != nil {
			log.Printf("encode image %s: %v", name, err)
		}
	})
}

// sessionState tracks the single lazily-started progress-DB session shared by
// all tool calls in this server process (see get_next_question below), plus
// the path of the last image opened via open_question_image.
type sessionState struct {
	id                int64
	started           bool
	lastExternalImage string
}

// killPreviousViewer best-effort kills whatever process opened the last
// externally-viewed image (the viewer command itself has already exited by
// the time we'd want to close it, so this matches on the temp file path in
// the target process's argv instead — catches viewers that take the path as
// a literal argument, e.g. feh/eog/sxiv, not browser- or portal-based
// handlers). No-op if nothing was opened yet.
func killPreviousViewer(sess *sessionState) {
	if sess.lastExternalImage == "" {
		return
	}
	_ = exec.Command("pkill", "-f", sess.lastExternalImage).Run()
	_ = os.Remove(sess.lastExternalImage)
	sess.lastExternalImage = ""
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
	TopicsAM    []string `json:"topicsAm,omitempty"`
	TopicsPM    []string `json:"topicsPm,omitempty"`
	TopicsOther []string `json:"topicsOther,omitempty"`
	Exams       []string `json:"exams"`
}

type getNextQuestionIn struct {
	Topic             string `json:"topic,omitempty" jsonschema:"filter by topic"`
	ExamID            string `json:"examId,omitempty" jsonschema:"filter by exam id"`
	Mode              string `json:"mode,omitempty" jsonschema:"random | review (only questions you most recently got wrong) | weak (weighted towards topics with lower accuracy, including ones you haven't tried yet), default random"`
	LightMode         bool   `json:"lightMode,omitempty" jsonschema:"if true, return the image with its original (light) colors instead of the default inverted (dark) version"`
	ContinueSessionID int64  `json:"continueSessionId,omitempty" jsonschema:"only meaningful on the first get_next_question call of a conversation (once a session is active, further calls just keep using it): attach to this not-completed session id instead of starting a new one, so answers keep accumulating in it — get its id from get_sessions with incompleteOnly=true. Its stored topic/examId become the defaults for this and later calls unless overridden."`
	RepeatSessionID   int64  `json:"repeatSessionId,omitempty" jsonschema:"only meaningful on the first get_next_question call of a conversation: start a brand-new session reusing another session's topic/examId/mode (get its id from get_sessions) — that session need not be incomplete. Mutually exclusive with continueSessionId."`
}

type getNextQuestionOut struct {
	SessionID  int64  `json:"sessionId" jsonschema:"pass verbatim to submit_answer"`
	QuestionID string `json:"questionId" jsonschema:"opaque id, pass verbatim to submit_answer"`
	ExamID     string `json:"examId"`
	Number     int    `json:"number" jsonschema:"question number within examId; pass both to get_question to look this exact question back up (e.g. to re-fetch, toggle lightMode, or reveal the answer)"`
	ImageURL   string `json:"imageUrl,omitempty" jsonschema:"the question text/diagrams/choices live ONLY in this image, not in any other field — YOU must fetch/view it before answering, and transcribe it to the user as-is (don't reword/paraphrase unless asked); also put this in a markdown image link so the user can see it"`
	ImageMode  string `json:"imageMode" jsonschema:"\"dark\" (colors inverted, the default) or \"light\" (original colors) — which one the imageUrl/embedded image actually is"`
}

type submitAnswerIn struct {
	SessionID   int64  `json:"sessionId" jsonschema:"session id from a prior get_next_question call in this conversation"`
	QuestionID  string `json:"questionId" jsonschema:"the opaque questionId returned by get_next_question"`
	Answer      string `json:"answer" jsonschema:"ONLY the answer letter the user themself just stated for this exact question — never call this tool with a letter the AI picked, guessed, or recalled from earlier context. If the user says (in any wording) that they don't know, or hasn't actually answered yet, pass the literal string \"idk\" instead of guessing on their behalf or relaying their exact words; \"idk\" is graded as incorrect but recorded distinctly from a wrong guess"`
	TimedOut    bool   `json:"timedOut,omitempty"`
	TimeTakenMs int    `json:"timeTakenMs,omitempty"`
}

type submitAnswerOut struct {
	Correct       bool   `json:"correct"`
	CorrectAnswer string `json:"correctAnswer,omitempty" jsonschema:"the correct answer letter — tell the user this when they got it wrong or didn't know"`
	Topic         string `json:"topic,omitempty"`
}

type undoLastAnswerIn struct {
	SessionID int64 `json:"sessionId,omitempty" jsonschema:"if set, only undo within this session; default 0 = the most recent attempt across all sessions"`
}

type undoLastAnswerOut struct {
	QuestionID string `json:"questionId"`
	ExamID     string `json:"examId"`
	Topic      string `json:"topic"`
	Answer     string `json:"answer"`
	Correct    bool   `json:"correct"`
	AnsweredAt string `json:"answeredAt"`
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
	ReviewQueue int            `json:"reviewQueue" jsonschema:"count of questions whose MOST RECENT attempt was wrong — a simple wrong-last-time list, not a spaced-repetition schedule; use mode=review on get_next_question to draw from it"`
	PartStats   []partStatOut  `json:"partStats"`
	TopicStats  []topicStatOut `json:"topicStats,omitempty"`
	ExamStats   []examStatOut  `json:"examStats,omitempty"`
}

type getQuestionIn struct {
	ExamID       string `json:"examId" jsonschema:"exam id, e.g. 2025A_FE-A"`
	Number       int    `json:"number" jsonschema:"question number within the exam"`
	RevealAnswer bool   `json:"revealAnswer,omitempty" jsonschema:"if true, include the correct answer (topic is also included, but not a canned explanation — explain it yourself)"`
	LightMode    bool   `json:"lightMode,omitempty" jsonschema:"if true, return the image with its original (light) colors instead of the default inverted (dark) version"`
}

type getQuestionOut struct {
	SessionID    int64            `json:"sessionId,omitempty" jsonschema:"pass verbatim to submit_answer; only set when revealAnswer is false, since a revealed answer isn't meant to be graded"`
	QuestionID   string           `json:"questionId"`
	ExamID       string           `json:"examId"`
	Number       int              `json:"number"`
	ImageURL     string           `json:"imageUrl,omitempty" jsonschema:"the question text/diagrams/choices live ONLY in this image, not in any other field — YOU must fetch/view it before answering, and transcribe it to the user as-is (don't reword/paraphrase unless asked); also put this in a markdown image link so the user can see it"`
	ImageMode    string           `json:"imageMode" jsonschema:"\"dark\" (colors inverted, the default) or \"light\" (original colors) — which one the imageUrl actually is"`
	Topic        string           `json:"topic,omitempty"`
	Answer       json.RawMessage  `json:"answer,omitempty"`
	SimpleAnswer string           `json:"simpleAnswer,omitempty"`
	SubAnswers   []core.SubAnswer `json:"subAnswers,omitempty"`
}

type openQuestionImageIn struct {
	ExamID    string `json:"examId" jsonschema:"exam id, e.g. 2025A_FE-A"`
	Number    int    `json:"number" jsonschema:"question number within the exam"`
	LightMode bool   `json:"lightMode,omitempty" jsonschema:"if true, open the original (light) colors instead of the default inverted (dark) version"`
}

type openQuestionImageOut struct {
	Opened    bool   `json:"opened"`
	Viewer    string `json:"viewer" jsonschema:"the local command used to open the image"`
	ImageMode string `json:"imageMode" jsonschema:"\"dark\" or \"light\" — which one was opened"`
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
	Scope          string `json:"scope,omitempty" jsonschema:"all | exam:<id> | part:am | part:pm, default all (topic scope not supported); ignored when incompleteOnly is true"`
	Order          string `json:"order,omitempty" jsonschema:"newest | oldest, default newest"`
	Limit          int    `json:"limit,omitempty" jsonschema:"max sessions to return, default 20"`
	IncompleteOnly bool   `json:"incompleteOnly,omitempty" jsonschema:"only return sessions that never finished cleanly (interrupted, or the process was killed before it could mark completion) — use to find a session id to pass as continueSessionId to get_next_question"`
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

// registerTools wires up all MCP tools. baseURL is nil for stdio (no HTTP
// server to serve images from); for --remote it holds the server's current
// public base URL (ngrok URL once the tunnel is up, else the local address),
// used to build an imageUrl alongside the embedded image content.
func registerTools(server *mcp.Server, c *core.Core, sess *sessionState, baseURL *atomic.Pointer[string], imageViewerCmd string) {
	imageURLFor := func(q *core.Question, lightMode bool) string {
		if baseURL == nil {
			return ""
		}
		bu := baseURL.Load()
		if bu == nil || *bu == "" {
			return ""
		}
		url := *bu + "/images/" + q.ImageRelPath()
		if lightMode {
			url += "?light=1"
		}
		return url
	}
	imageModeFor := func(lightMode bool) string {
		if lightMode {
			return "light"
		}
		return "dark"
	}
	// embedImageFor returns the question image embedded as base64 content,
	// always in original (light) colors regardless of the dark-mode default
	// used for imageUrl/imageMode — this is a fallback for MCP clients that
	// can't fetch imageUrl, so it needs to be readable on its own without
	// requiring the caller to also know to pass lightMode.
	embedImageFor := func(q *core.Question) (*mcp.ImageContent, error) {
		img, err := c.Bank.Image(q)
		if err != nil {
			return nil, fmt.Errorf("load image: %w", err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("encode image: %w", err)
		}
		return &mcp.ImageContent{Data: buf.Bytes(), MIMEType: "image/png"}, nil
	}

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
					Mode: orDefault(in.Mode, params.Mode), OrderStrategy: "random",
				})
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				sess.id = id
				sess.started = true
			default:
				id, err := c.StartSession(ctx, core.SessionParams{
					ExamType: "fe", ExamID: examID, Topic: topic,
					Mode: orDefault(in.Mode, "normal"), OrderStrategy: "random",
				})
				if err != nil {
					return nil, getNextQuestionOut{}, err
				}
				sess.id = id
				sess.started = true
			}
		}
		q, err := c.GetNextQuestion(ctx, core.QuestionFilter{Topic: topic, ExamID: examID, Mode: orDefault(in.Mode, "random")})
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		imgContent, err := embedImageFor(q)
		if err != nil {
			return nil, getNextQuestionOut{}, err
		}
		out := getNextQuestionOut{
			SessionID: sess.id, QuestionID: q.GlobalID(), ExamID: q.ExamID, Number: q.ID,
			ImageURL: imageURLFor(q, in.LightMode), ImageMode: imageModeFor(in.LightMode),
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
			ImageURL: imageURLFor(q, in.LightMode), ImageMode: imageModeFor(in.LightMode),
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
		imgContent, err := embedImageFor(q)
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
		if err := exec.Command(imageViewerCmd, tmp.Name()).Start(); err != nil {
			return nil, openQuestionImageOut{}, fmt.Errorf("%s %s: %w", imageViewerCmd, tmp.Name(), err)
		}
		sess.lastExternalImage = tmp.Name()

		return nil, openQuestionImageOut{Opened: true, Viewer: imageViewerCmd, ImageMode: imageModeFor(in.LightMode)}, nil
	})

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
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		var records []core.SessionRecord
		var err error
		if in.IncompleteOnly {
			records, err = c.IncompleteSessions(ctx, limit)
		} else {
			scope := core.Scope(orDefault(in.Scope, "all"))
			order := core.HistoryOrder(orDefault(in.Order, "newest"))
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
