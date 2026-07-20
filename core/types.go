package core

import (
	"time"

	"github.com/jim-ww/itpec-sensei/examdata"
)

// SubAnswer, Explanation, Question, ExamInfo, and ExamData are the wire
// format of the downloaded question bank — see package examdata, the single
// source of truth for that JSON schema. Aliased here so existing callers can
// keep referring to core.Question etc.
type (
	SubAnswer   = examdata.SubAnswer
	Explanation = examdata.Explanation
	Question    = examdata.Question
	ExamInfo    = examdata.ExamInfo
	ExamData    = examdata.ExamData
)

// QuestionFilter narrows GetNextQuestion selection.
type QuestionFilter struct {
	Topic  string   // optional
	ExamID string   // optional
	Tags   []string // optional, match-any (see FilterByTags)
	Mode   string   // "random" | "review" | "weak" | "sequential"
	// ExcludeIDs are Question.GlobalID()s to skip, e.g. questions already
	// answered in the caller's current session — so "sequential" can advance
	// past them, and so repeats aren't handed back in other modes either.
	ExcludeIDs []string
}

// Scope narrows progress queries / resets: "all", "topic:<name>", "tag:<name>",
// "exam:<id>", or "part:am" / "part:pm" (FE-AM/FE-A vs FE-PM/FE-B exam
// session). GetSessions is the one exception — it rejects topic/tag scope,
// since a session isn't inherently scoped to one (see validateSessionScope).
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

// ExamDetail is the readable "get_exam" view: exam metadata plus the user's
// own progress on it.
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
