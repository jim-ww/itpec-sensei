package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeIdk(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"question mark sentinel", "?", "idk"},
		{"lowercase idk", "idk", "idk"},
		{"uppercase IDK", "IDK", "idk"},
		{"mixed case IdK", "IdK", "idk"},
		{"regular answer untouched", "A", "A"},
		{"empty string untouched", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeIdk(tt.in))
		})
	}
}

func TestGradeAnswer(t *testing.T) {
	simple := &Question{SimpleAnswer: "A"}
	multiPart := &Question{SubAnswers: []SubAnswer{{SQ: 1, Answer: "c"}, {SQ: 2, Answer: "d"}}}
	noAnswer := &Question{}

	tests := []struct {
		name   string
		q      *Question
		answer string
		want   bool
	}{
		{"exact match", simple, "A", true},
		{"case insensitive match", simple, "a", true},
		{"whitespace trimmed", simple, "  A  ", true},
		{"wrong letter", simple, "B", false},
		{"idk sentinel never matches", simple, "idk", false},
		{"multi-part matches first sub-answer", multiPart, "c", true},
		{"multi-part case insensitive", multiPart, "C", true},
		{"multi-part wrong letter", multiPart, "d", false},
		{"question with no answer never grades correct", noAnswer, "", false},
		{"question with no answer never grades correct nonempty", noAnswer, "A", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, gradeAnswer(tt.q, tt.answer))
		})
	}
}

func TestCorrectAnswerLabel(t *testing.T) {
	tests := []struct {
		name string
		q    *Question
		want string
	}{
		{"simple answer wins", &Question{SimpleAnswer: "A", SubAnswers: []SubAnswer{{Answer: "z"}}}, "A"},
		{"falls back to first sub-answer", &Question{SubAnswers: []SubAnswer{{SQ: 1, Answer: "c"}, {SQ: 2, Answer: "d"}}}, "c"},
		{"neither set returns empty", &Question{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, correctAnswerLabel(tt.q))
		})
	}
}
