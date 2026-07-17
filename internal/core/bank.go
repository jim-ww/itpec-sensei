package core

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Bank is the in-memory, read-only index over the downloaded question set.
type Bank struct {
	fsys     fs.FS
	byID     map[string]*Question // keyed by Question.GlobalID()
	byExam   map[string][]*Question
	byTopic  map[string][]*Question
	examInfo map[string]ExamInfo
	exams    []string
	topics   []string
	// topicParts maps each topic to the exam part ("am"/"pm") all of its
	// questions come from. Topics whose questions span more than one part (or
	// come from a part-less exam like IT Passport) map to "", grouped as
	// "other" by TopicsByPart.
	topicParts map[string]string
}

// LoadBank parses every exam JSON file under dataDir (the "questions" directory
// produced by extracting the downloaded data archive, see DefaultDataDir) into
// memory and builds lookup indexes.
func LoadBank(dataDir string) (*Bank, error) {
	sub := os.DirFS(dataDir)

	b := &Bank{
		fsys:     sub,
		byID:     make(map[string]*Question),
		byExam:   make(map[string][]*Question),
		byTopic:  make(map[string][]*Question),
		examInfo: make(map[string]ExamInfo),
	}

	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, fmt.Errorf("bank: read dir: %w", err)
	}

	examSet := make(map[string]struct{})
	topicSet := make(map[string]struct{})
	topicPartSet := make(map[string]map[string]struct{})

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
		b.examInfo[examID] = data.ExamInfo

		for i := range data.Questions {
			q := &data.Questions[i]
			q.ExamID = examID
			b.byID[q.GlobalID()] = q
			b.byExam[examID] = append(b.byExam[examID], q)
			topic := q.Topic()
			topicSet[topic] = struct{}{}
			b.byTopic[topic] = append(b.byTopic[topic], q)
			if topicPartSet[topic] == nil {
				topicPartSet[topic] = make(map[string]struct{})
			}
			topicPartSet[topic][ExamPart(examID)] = struct{}{}
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

	b.topicParts = make(map[string]string, len(topicPartSet))
	for t, parts := range topicPartSet {
		if len(parts) == 1 {
			for p := range parts {
				b.topicParts[t] = p
			}
		}
	}

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

// ExamInfo returns the scraped metadata (name, date, duration, question
// count) for one exam ID, or false if the exam ID is unknown.
func (b *Bank) ExamInfo(examID string) (ExamInfo, bool) {
	info, ok := b.examInfo[examID]
	return info, ok
}

// Exams returns all known exam IDs, sorted.
func (b *Bank) Exams() []string {
	return b.exams
}

// Topics returns all known topics, sorted.
func (b *Bank) Topics() []string {
	return b.topics
}

// TopicsByPart groups all known topics (each already sorted, since b.topics
// is sorted) by the exam part their questions come from. Topics with no
// single-part association — e.g. IT Passport-only topics, or ones spanning
// both AM and PM — are grouped under other.
func (b *Bank) TopicsByPart() (am, pm, other []string) {
	for _, t := range b.topics {
		switch b.topicParts[t] {
		case "am":
			am = append(am, t)
		case "pm":
			pm = append(pm, t)
		default:
			other = append(other, t)
		}
	}
	return am, pm, other
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

// InvertImage flips every pixel's RGB channels (alpha untouched), turning a
// light-background question image into a dark one. Shared by the CLI's
// sixel/xdg-open rendering and the MCP server's embedded image content, so
// "dark mode" behaves identically everywhere a question image is displayed.
func InvertImage(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			dst.Set(x, y, color.RGBA{
				R: 255 - uint8(r>>8),
				G: 255 - uint8(g>>8),
				B: 255 - uint8(bl>>8),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

// ImagesFS returns the embedded "images" subtree, for serving question images
// over HTTP (e.g. to remote MCP clients that can't read the embedded binary).
func (b *Bank) ImagesFS() (fs.FS, error) {
	return fs.Sub(b.fsys, "images")
}
