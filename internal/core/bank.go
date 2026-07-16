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
	"strings"
)

//go:embed all:data/questions
var embeddedData embed.FS

// Bank is the in-memory, read-only index over the embedded question set.
type Bank struct {
	fsys    fs.FS
	byID    map[int]*Question
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
		byID:    make(map[int]*Question),
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
			b.byID[q.ID] = q
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

// Question returns the question with the given ID, or nil if not found.
func (b *Bank) Question(id int) *Question {
	return b.byID[id]
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
	sort.Slice(pool, func(i, j int) bool { return pool[i].ID < pool[j].ID })
	return pool
}

// Image lazily decodes and returns the image for a question.
func (b *Bank) Image(q *Question) (image.Image, error) {
	name := path.Join("images", q.ExamID, path.Base(q.ImageURL))
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
