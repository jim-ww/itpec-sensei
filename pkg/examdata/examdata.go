// Package examdata is the wire format shared by cmd/scraper (which produces
// it, one JSON file per exam) and internal/core (which loads it back into a
// Bank) — the single source of truth for that JSON schema, so the producer
// and consumer can't silently drift apart.
package examdata

import (
	"encoding/json"
	"path"
	"strconv"
)

// SubAnswer is one sub-part answer for multi-part questions where "answer" is
// an array of sub-question answers instead of a single letter, e.g.
// {"sq":1,"bq":"A","answer":"c"}.
type SubAnswer struct {
	SQ     int    `json:"sq"`
	BQ     string `json:"bq,omitempty"`
	Answer string `json:"answer"`
}

// Explanation is community/admin-authored context for one question: its
// topic and a markdown explanation of the answer.
type Explanation struct {
	Topic       string `json:"topic"`
	Explanation string `json:"explanation"`
}

// Question is one exam question. Answer is left as raw JSON because it is
// polymorphic: a plain string for single-answer questions, or an array of
// SubAnswer for multi-part questions — see DecodeAnswer.
type Question struct {
	ID             int             `json:"id"`
	Answer         json.RawMessage `json:"answer,omitempty"`
	ImageURL       string          `json:"image"`
	ExamID         string          `json:"examId"`
	LocalImagePath string          `json:"localImagePath,omitempty"`
	Explanation    *Explanation    `json:"explanation,omitempty"`

	// Decoded convenience fields, populated by DecodeAnswer.
	SimpleAnswer string      `json:"simpleAnswer,omitempty"`
	SubAnswers   []SubAnswer `json:"subAnswers,omitempty"`
}

// DecodeAnswer figures out which shape Answer is and fills in SimpleAnswer or
// SubAnswers accordingly. The scraper calls this once after fetching each
// question, so that by the time a Question reaches disk (and later, a
// Bank), Answer's polymorphism has already been resolved.
func (q *Question) DecodeAnswer() {
	if len(q.Answer) == 0 {
		return
	}
	var s string
	if err := json.Unmarshal(q.Answer, &s); err == nil {
		q.SimpleAnswer = s
		return
	}
	var subs []SubAnswer
	if err := json.Unmarshal(q.Answer, &subs); err == nil {
		q.SubAnswers = subs
	}
}

// Topic returns the question's topic, falling back to "Uncategorized" when no
// explanation (and therefore no topic) was scraped for it.
func (q *Question) Topic() string {
	if q.Explanation == nil || q.Explanation.Topic == "" {
		return "Uncategorized"
	}
	return q.Explanation.Topic
}

// GlobalID returns a globally unique identifier for this question. ID alone
// is only unique within a single exam (every exam's first question is id 1),
// so anything that needs to address a question across exams — the progress
// DB, MCP tool calls, cross-exam ordering — must use this instead.
func (q *Question) GlobalID() string {
	return q.ExamID + "#" + strconv.Itoa(q.ID)
}

// ImageRelPath returns the question's image path relative to the "images"
// directory (e.g. "2018A_FE-AM/q1.png"), suitable for both local embedded-FS
// lookups and as a URL path segment when serving images over HTTP.
func (q *Question) ImageRelPath() string {
	return path.Join(q.ExamID, path.Base(q.ImageURL))
}

// ExamInfo is exam-level metadata embedded alongside the questions.
type ExamInfo struct {
	Exam            string `json:"exam"`
	Date            string `json:"date"`
	TotalQuestions  int    `json:"totalQuestions"`
	DurationMinutes int    `json:"durationMinutes"`
}

// ExamData is everything scraped for one exam — the top-level shape of one
// exam JSON file.
type ExamData struct {
	ExamID         string     `json:"examId"`
	ScraperVersion int        `json:"scraperVersion"`
	ExamInfo       ExamInfo   `json:"examInfo"`
	Questions      []Question `json:"questions"`
}
