package core

import (
	"embed"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed all:data/questions
var embeddedData embed.FS

// Bank is the in-memory, read-only index over the embedded question set.
type Bank struct {
	fsys    fs.FS
	byID    map[string]*Question // keyed by Question.GlobalID()
	byExam  map[string][]*Question
	byTopic map[string][]*Question
	exams   []string
	topics  []string
}

// LoadBank parses every embedded exam JSON file into memory and builds lookup indexes.
func LoadBank() (*Bank, error) {
	sub, err := fs.Sub(embeddedData, "data/questions")
	if err != nil {
		return nil, fmt.Errorf("bank: sub fs: %w", err)
	}

	b := &Bank{
		fsys:    sub,
		byID:    make(map[string]*Question),
		byExam:  make(map[string][]*Question),
		byTopic: make(map[string][]*Question),
	}

	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, fmt.Errorf("bank: read dir: %w", err)
	}

	examSet := make(map[string]struct{})
	topicSet := make(map[string]struct{})

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := fs.ReadFile(sub, e.Name())
		if err != nil {
			return nil, fmt.Errorf("bank: read %s: %w", e.Name(), err)
		}
		var data ExamData
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("bank: parse %s: %w", e.Name(), err)
		}
		examID := data.ExamID
		if examID == "" {
			examID = strings.TrimSuffix(e.Name(), ".json")
		}
		examSet[examID] = struct{}{}

		for i := range data.Questions {
			q := &data.Questions[i]
			q.ExamID = examID
			b.byID[q.GlobalID()] = q
			b.byExam[examID] = append(b.byExam[examID], q)
			topic := q.Topic()
			topicSet[topic] = struct{}{}
			b.byTopic[topic] = append(b.byTopic[topic], q)
		}
	}

	for id := range examSet {
		b.exams = append(b.exams, id)
	}
	sort.Strings(b.exams)
	for t := range topicSet {
		b.topics = append(b.topics, t)
	}
	sort.Strings(b.topics)

	return b, nil
}

// Question returns the question with the given global ID (Question.GlobalID()),
// or nil if not found.
func (b *Bank) Question(globalID string) *Question {
	return b.byID[globalID]
}

// QuestionByExamAndNumber looks up a question by its exam ID and per-exam
// question number (as printed to users, e.g. "q34"), rather than by the
// opaque GlobalID.
func (b *Bank) QuestionByExamAndNumber(examID string, number int) *Question {
	return b.byID[examID+"#"+strconv.Itoa(number)]
}

// Exams returns all known exam IDs, sorted.
func (b *Bank) Exams() []string {
	return b.exams
}

// Topics returns all known topics, sorted.
func (b *Bank) Topics() []string {
	return b.topics
}

// Questions returns questions matching the given filter (topic and/or exam ID; empty = no filter).
func (b *Bank) Questions(topic, examID string) []*Question {
	var pool []*Question
	switch {
	case topic != "" && examID != "":
		for _, q := range b.byTopic[topic] {
			if q.ExamID == examID {
				pool = append(pool, q)
			}
		}
	case topic != "":
		pool = b.byTopic[topic]
	case examID != "":
		pool = b.byExam[examID]
	default:
		pool = make([]*Question, 0, len(b.byID))
		for _, q := range b.byID {
			pool = append(pool, q)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].GlobalID() < pool[j].GlobalID() })
	return pool
}

// QuestionsForExams returns all questions across the given exam IDs, sorted by ID.
func (b *Bank) QuestionsForExams(examIDs []string) []*Question {
	var pool []*Question
	for _, id := range examIDs {
		pool = append(pool, b.byExam[id]...)
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].GlobalID() < pool[j].GlobalID() })
	return pool
}

// ExamPart returns "am" or "pm" for an exam ID whose code ends in one of the
// AM/PM (pre-2024 format) or A/B (2024-onward format) session suffixes, or ""
// for exams with no AM/PM distinction (e.g. IT Passport).
func ExamPart(examID string) string {
	_, code, ok := strings.Cut(examID, "_")
	if !ok {
		code = examID
	}
	switch {
	case strings.HasSuffix(code, "-AM"), strings.HasSuffix(code, "-A"):
		return "am"
	case strings.HasSuffix(code, "-PM"), strings.HasSuffix(code, "-B"):
		return "pm"
	default:
		return ""
	}
}

// ExamsByPart returns all known exam IDs whose ExamPart matches part
// ("am" | "pm"), or all exam IDs if part is "" (no filtering).
func (b *Bank) ExamsByPart(part string) []string {
	if part == "" {
		return b.exams
	}
	var matched []string
	for _, id := range b.exams {
		if ExamPart(id) == part {
			matched = append(matched, id)
		}
	}
	return matched
}

// ImageRelPath returns the question's image path relative to the "images"
// directory (e.g. "2018A_FE-AM/q1.png"), suitable for both local embedded-FS
// lookups and as a URL path segment when serving images over HTTP.
func (q *Question) ImageRelPath() string {
	return path.Join(q.ExamID, path.Base(q.ImageURL))
}

// Image lazily decodes and returns the image for a question.
func (b *Bank) Image(q *Question) (image.Image, error) {
	name := path.Join("images", q.ImageRelPath())
	f, err := b.fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("bank: open image %s: %w", name, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("bank: decode image %s: %w", name, err)
	}
	return img, nil
}

// ImagesFS returns the embedded "images" subtree, for serving question images
// over HTTP (e.g. to remote MCP clients that can't read the embedded binary).
func (b *Bank) ImagesFS() (fs.FS, error) {
	return fs.Sub(b.fsys, "images")
}
