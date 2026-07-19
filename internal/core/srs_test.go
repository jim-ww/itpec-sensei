package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextSRSState(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		box       int
		correct   bool
		wantBox   int
		wantDueIn time.Duration
	}{
		{"first attempt, correct, advances to box 2", 1, true, 2, 3 * 24 * time.Hour},
		{"box 2 correct advances to box 3", 2, true, 3, 7 * 24 * time.Hour},
		{"box 4 correct advances to box 5", 4, true, 5, 60 * 24 * time.Hour},
		{"box 5 correct stays at box 5 (max)", 5, true, 5, 60 * 24 * time.Hour},
		{"any box wrong resets to box 1", 3, false, 1, 24 * time.Hour},
		{"box 5 wrong resets to box 1", 5, false, 1, 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextSRSState(tt.box, tt.correct, now)
			assert.Equal(t, tt.wantBox, got.Box)
			assert.Equal(t, now.Add(tt.wantDueIn), got.DueAt)
			assert.Equal(t, now, got.LastReviewedAt)
		})
	}
}
