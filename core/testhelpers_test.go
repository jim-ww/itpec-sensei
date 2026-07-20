package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestBank builds a Bank over a small fixture question set, covering both
// exam parts:
//   - 2020A_FE-A (AM part, suffix "-A"): q1 topic "Networks" answer "A",
//     q2 topic "Security" answer "B".
//   - 2020A_FE-B (PM part, suffix "-B"): q1 topic "Databases" answer "C".
func newTestBank(t *testing.T) *Bank {
	t.Helper()
	dir := t.TempDir()

	exams := []ExamData{
		{
			ExamID:   "2020A_FE-A",
			ExamInfo: ExamInfo{Exam: "2020 Spring FE AM", Date: "2020-04-19", TotalQuestions: 2, DurationMinutes: 90},
			Questions: []Question{
				{ID: 1, ImageURL: "q1.png", Explanation: &Explanation{Topic: "Networks", Explanation: "net explanation"}, SimpleAnswer: "A"},
				{ID: 2, ImageURL: "q2.png", Explanation: &Explanation{Topic: "Security", Explanation: "sec explanation"}, SimpleAnswer: "B"},
			},
		},
		{
			ExamID:   "2020A_FE-B",
			ExamInfo: ExamInfo{Exam: "2020 Spring FE PM", Date: "2020-04-19", TotalQuestions: 1, DurationMinutes: 120},
			Questions: []Question{
				{ID: 1, ImageURL: "q1.png", Explanation: &Explanation{Topic: "Databases", Explanation: "db explanation"}, SimpleAnswer: "C"},
			},
		},
	}

	for _, e := range exams {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, e.ExamID+".json"), b, 0o644))
	}

	bank, err := LoadBank(dir)
	require.NoError(t, err)
	return bank
}
