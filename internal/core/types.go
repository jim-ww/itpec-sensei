package core

import "encoding/json"

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

// Scope narrows progress queries / resets: "all", "topic:<name>", or "exam:<id>".
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
	Correct     bool         `json:"correct"`
	Explanation *Explanation `json:"explanation,omitempty"`
}

// TopicStat is one row of per-topic aggregate stats.
type TopicStat struct {
	Topic    string  `json:"topic"`
	Answered int     `json:"answered"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// ProgressSummary is the aggregate progress view.
type ProgressSummary struct {
	Answered    int            `json:"answered"`
	Correct     int            `json:"correct"`
	Accuracy    float64        `json:"accuracy"`
	Streak      int            `json:"streak"`
	ReviewQueue int            `json:"reviewQueue"`
	Heatmap     map[string]int `json:"heatmap"` // date (YYYY-MM-DD) -> answers that day
}
