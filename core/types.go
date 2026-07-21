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
	// Unanswered restricts the pool to questions never answered in any
	// session, not just the current one.
	Unanswered bool
}

// Scope is the single-dimension "all"/"topic:<name>"/"tag:<name>"/
// "exam:<id>"/"part:am|pm" scope string used only by ResetProgress (see
// ParseScope) — kept as its own deliberately terse, hard-to-mistype
// mini-syntax for a destructive command. Everywhere else that narrows by
// scope (progress queries) uses the combinable ScopeFilter instead.
type Scope string

const (
	ScopeAll = Scope("all")
)

// ScopeFilter narrows progress queries (GetProgressSummary, GetTopicStats,
// GetTagStats, GetExamStats, GetHistory, GetSessions) by topic/tag/exam/part
// combined with AND, mirroring QuestionFilter's combinability. All fields
// empty means "no filter". GetSessions is the one exception — it rejects a
// non-empty Topic/Tag, since a session isn't inherently scoped to one (see
// validateSessionScope).
type ScopeFilter struct {
	Topic  string
	Tags   []string // match-any, like QuestionFilter.Tags
	ExamID string
	Part   string // "am" | "pm" | ""
}

// IsEmpty reports whether filter narrows nothing (equivalent to the old
// "all" scope).
func (f ScopeFilter) IsEmpty() bool {
	return f.Topic == "" && len(f.Tags) == 0 && f.ExamID == "" && f.Part == ""
}

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

// TagStat is one row of per-tag aggregate stats, mirroring TopicStat. Unlike
// topic (one per question), a question can carry several tags, so a single
// attempt contributes to every tag stat its question has (see GetTagStats).
type TagStat struct {
	Tag      string  `json:"tag"`
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
	Tags        []string  `json:"tags,omitempty"`
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
