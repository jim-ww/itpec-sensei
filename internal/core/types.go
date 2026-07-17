package core

import (
	"encoding/json"
	"strconv"
	"time"
)

// SubAnswer represents one sub-part answer for multi-part exams (e.g. old FE-PM format).
type SubAnswer struct {
	SQ     int    `json:"sq"`
	BQ     string `json:"bq,omitempty"`
	Answer string `json:"answer"`
}

// Explanation holds the topic and markdown explanation text for a question.
type Explanation struct {
	Topic       string `json:"topic"`
	Explanation string `json:"explanation"`
}

// Question is a single scraped question, as produced by cmd/scraper.
type Question struct {
	ID             int             `json:"id"`
	Answer         json.RawMessage `json:"answer,omitempty"`
	ImageURL       string          `json:"image"`
	ExamID         string          `json:"examId"`
	LocalImagePath string          `json:"localImagePath,omitempty"`
	Explanation    *Explanation    `json:"explanation,omitempty"`
	SimpleAnswer   string          `json:"simpleAnswer,omitempty"`
	SubAnswers     []SubAnswer     `json:"subAnswers,omitempty"`
}

// Topic returns the question's topic, falling back to "Uncategorized" when no
// explanation (and therefore no topic) was scraped for it.
func (q *Question) Topic() string {
	if q.Explanation == nil || q.Explanation.Topic == "" {
		return "Uncategorized"
	}
	return q.Explanation.Topic
}

// GlobalID returns a globally unique identifier for this question. Question.ID
// alone is only unique within a single exam (every exam's first question is
// id 1), so anything that needs to address a question across exams — the
// progress DB, MCP tool calls, cross-exam ordering — must use this instead.
func (q *Question) GlobalID() string {
	return q.ExamID + "#" + strconv.Itoa(q.ID)
}

// ExamInfo describes exam-level metadata.
type ExamInfo struct {
	Exam            string `json:"exam"`
	Date            string `json:"date"`
	TotalQuestions  int    `json:"totalQuestions"`
	DurationMinutes int    `json:"durationMinutes"`
}

// ExamData is the top-level shape of one scraped exam JSON file.
type ExamData struct {
	ExamID         string     `json:"examId"`
	ScraperVersion int        `json:"scraperVersion"`
	ExamInfo       ExamInfo   `json:"examInfo"`
	Questions      []Question `json:"questions"`
}

// QuestionFilter narrows GetNextQuestion selection.
type QuestionFilter struct {
	Topic  string // optional
	ExamID string // optional
	Mode   string // "random" | "review"
}

// Scope narrows progress queries / resets: "all", "topic:<name>", "exam:<id>",
// or "part:am" / "part:pm" (FE-AM/FE-A vs FE-PM/FE-B exam session).
type Scope string

const (
	ScopeAll = Scope("all")
)

// Period narrows progress summary time window.
type Period string

const (
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
	PeriodAll   Period = "all"
)

// AnswerResult is returned by SubmitAnswer.
type AnswerResult struct {
	Correct       bool         `json:"correct"`
	CorrectAnswer string       `json:"correctAnswer,omitempty"`
	Explanation   *Explanation `json:"explanation,omitempty"`
}

// TopicStat is one row of per-topic aggregate stats.
type TopicStat struct {
	Topic    string  `json:"topic"`
	Answered int     `json:"answered"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// ExamStat is one row of per-exam aggregate stats, mirroring TopicStat.
type ExamStat struct {
	ExamID   string  `json:"examId"`
	Answered int     `json:"answered"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// ExamDetail is the readable "get_exam" view: scraped exam metadata plus the
// user's own progress on it.
type ExamDetail struct {
	ExamID                   string  `json:"examId"`
	Name                     string  `json:"name,omitempty"`
	Date                     string  `json:"date,omitempty"`
	Part                     string  `json:"part,omitempty"` // "am" | "pm" | ""
	DurationMinutes          int     `json:"durationMinutes,omitempty"`
	TotalQuestions           int     `json:"totalQuestions"`
	TargetSecondsPerQuestion int     `json:"targetSecondsPerQuestion,omitempty"` // pacing target implied by the real exam's time limit
	Answered                 int     `json:"answered"`
	Correct                  int     `json:"correct"`
	Accuracy                 float64 `json:"accuracy"`
	AvgTimeMs                float64 `json:"avgTimeMs,omitempty"` // your own average answer time on this exam's questions
}

// PartStat is per-part (AM/PM, or "other" for exams with no AM/PM split, e.g.
// IT Passport) aggregate accuracy and timing. AM and PM sections test very
// different material, so they're never blended into one combined number.
type PartStat struct {
	Part         string  `json:"part"` // "am" | "pm" | "other"
	Answered     int     `json:"answered"`
	Correct      int     `json:"correct"`
	Accuracy     float64 `json:"accuracy"`
	AvgTimeMs    float64 `json:"avgTimeMs,omitempty"`
	MedianTimeMs float64 `json:"medianTimeMs,omitempty"`
}

// AttemptRecord is one past attempt, joined with question metadata, for history views.
type AttemptRecord struct {
	QuestionID  string    `json:"questionId"`
	ExamID      string    `json:"examId"`
	Topic       string    `json:"topic"`
	Answer      string    `json:"answer"`
	Correct     bool      `json:"correct"`
	TimedOut    bool      `json:"timedOut"`
	TimeTakenMs int       `json:"timeTakenMs,omitempty"`
	AnsweredAt  time.Time `json:"answeredAt"`
}

// SessionRecord is one past practice session, with its aggregate score
// computed by joining back to its attempts.
type SessionRecord struct {
	ID                       int64      `json:"id"`
	StartedAt                time.Time  `json:"startedAt"`
	EndedAt                  *time.Time `json:"endedAt,omitempty"`
	ExamType                 string     `json:"examType"`
	ExamID                   string     `json:"examId,omitempty"`
	Mode                     string     `json:"mode"`
	OrderStrategy            string     `json:"orderStrategy"`
	TimeLimitSeconds         *int       `json:"timeLimitSeconds,omitempty"`
	QuestionTimeLimitSeconds *int       `json:"questionTimeLimitSeconds,omitempty"`
	ExitReason               string     `json:"exitReason,omitempty"`
	Answered                 int        `json:"answered"`
	Correct                  int        `json:"correct"`
}

// SessionParams is the full set of parameters a practice session was planned
// with, plus the exact ordered question list drawn for it. Stored at session
// start so a session can later be resumed exactly (--continue) or repeated
// with a fresh draw of the same filters (--repeat).
type SessionParams struct {
	ExamType                 string
	ExamID                   string
	Topic                    string
	Part                     string
	Mode                     string
	OrderStrategy            string
	QuestionLimit            int // 0 = no limit
	QuestionNumber           int // 0 = not a single-question session
	TimeLimitSeconds         *int
	QuestionTimeLimitSeconds *int
	PlannedQuestions         []string // ordered Question.GlobalID()s
}

// ProgressSummary is the aggregate progress view. Per-part correctness and
// timing live in PartStats (see PartStat) rather than as blended totals here,
// since AM and PM sections test very different material.
type ProgressSummary struct {
	Answered    int            `json:"answered"`
	Streak      int            `json:"streak"`
	MaxStreak   int            `json:"maxStreak"`
	ReviewQueue int            `json:"reviewQueue"`
	PartStats   []PartStat     `json:"partStats"`
	Heatmap     map[string]int `json:"heatmap"` // date (YYYY-MM-DD) -> answers that day
}
