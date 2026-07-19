package mcpserver

import (
	"encoding/json"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

type listTopicsOut struct {
	TopicsAM    []string `json:"topicsAm,omitempty"`
	TopicsPM    []string `json:"topicsPm,omitempty"`
	TopicsOther []string `json:"topicsOther,omitempty"`
	Exams       []string `json:"exams"`
}

type getNextQuestionIn struct {
	Topic             string `json:"topic,omitempty" jsonschema:"filter by topic"`
	ExamID            string `json:"examId,omitempty" jsonschema:"filter by exam id"`
	Mode              string `json:"mode,omitempty" jsonschema:"random | review (spaced repetition: questions due under a Leitner-box schedule based on your answer history) | weak (weighted towards topics with lower accuracy, including ones you haven't tried yet) | sequential (lowest-numbered question first, skipping ones already answered this session — advances through the pool in order across calls), default random"`
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
	ReviewQueue int            `json:"reviewQueue" jsonschema:"count of questions currently due under the spaced-repetition (Leitner-box) schedule; use mode=review on get_next_question to draw from it"`
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
