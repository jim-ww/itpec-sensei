// Package core_test holds Core tests that only need its exported API,
// exercised against a real temp-file SQLite repository instead of a mock —
// see newTestRepo. Tests that reach into core's unexported functions
// (grading, scoping, streak math, ...) stay in package core itself (see
// e.g. answers_test.go, history_test.go, stats_test.go, questions_test.go).
package core_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jim-ww/itpec-sensei/core"
	"github.com/jim-ww/itpec-sensei/sqlite"
)

// newTestBank builds a Bank over a small fixture question set, covering both
// exam parts:
//   - 2020A_FE-A (AM part, suffix "-A"): q1 topic "Networks" answer "A",
//     q2 topic "Security" answer "B".
//   - 2020A_FE-B (PM part, suffix "-B"): q1 topic "Databases" answer "C".
func newTestBank(t *testing.T) *core.Bank {
	t.Helper()
	dir := t.TempDir()

	exams := []core.ExamData{
		{
			ExamID:   "2020A_FE-A",
			ExamInfo: core.ExamInfo{Exam: "2020 Spring FE AM", Date: "2020-04-19", TotalQuestions: 2, DurationMinutes: 90},
			Questions: []core.Question{
				{ID: 1, ImageURL: "q1.png", Explanation: &core.Explanation{Topic: "Networks", Explanation: "net explanation"}, SimpleAnswer: "A"},
				{ID: 2, ImageURL: "q2.png", Explanation: &core.Explanation{Topic: "Security", Explanation: "sec explanation"}, SimpleAnswer: "B"},
			},
		},
		{
			ExamID:   "2020A_FE-B",
			ExamInfo: core.ExamInfo{Exam: "2020 Spring FE PM", Date: "2020-04-19", TotalQuestions: 1, DurationMinutes: 120},
			Questions: []core.Question{
				{ID: 1, ImageURL: "q1.png", Explanation: &core.Explanation{Topic: "Databases", Explanation: "db explanation"}, SimpleAnswer: "C"},
			},
		},
	}

	for _, e := range exams {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, e.ExamID+".json"), b, 0o644))
	}

	bank, err := core.LoadBank(dir)
	require.NoError(t, err)
	return bank
}

// newTestRepo opens a real temp-file SQLite-backed Repository for tests, so
// Core's logic is exercised against actual SQL instead of a stubbed
// interface — see sqlite/repo_test.go for the equivalent at the repository
// layer itself.
func newTestRepo(t *testing.T) core.Repository {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "progress.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return sqlite.NewRepository(db)
}
