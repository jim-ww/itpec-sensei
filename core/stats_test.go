package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTimeStats(t *testing.T) {
	tests := []struct {
		name       string
		ms         []int
		wantAvg    float64
		wantMedian float64
	}{
		{"empty returns zero", nil, 0, 0},
		{"single value", []int{100}, 100, 100},
		{"odd count uses middle element", []int{100, 200, 300}, 200, 200},
		{"even count averages the two middle elements", []int{100, 200, 300, 400}, 250, 250},
		{"unsorted input still sorted for median", []int{300, 100, 200}, 200, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avg, median := timeStats(tt.ms)
			assert.Equal(t, tt.wantAvg, avg)
			assert.Equal(t, tt.wantMedian, median)
		})
	}
}

func day(offsetFromToday int) string {
	return time.Now().UTC().AddDate(0, 0, offsetFromToday).Format("2006-01-02")
}

func TestComputeStreak(t *testing.T) {
	tests := []struct {
		name    string
		heatmap map[string]int
		want    int
	}{
		{"no activity at all", map[string]int{}, 0},
		{"active today only", map[string]int{day(0): 1}, 1},
		{"active today and yesterday", map[string]int{day(0): 1, day(-1): 2}, 2},
		{"gap breaks the streak", map[string]int{day(0): 1, day(-2): 1}, 1},
		{"no activity today, anchors on yesterday", map[string]int{day(-1): 1, day(-2): 1}, 2},
		{"no activity today or yesterday", map[string]int{day(-2): 1}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeStreak(tt.heatmap))
		})
	}
}

func TestComputeMaxStreak(t *testing.T) {
	tests := []struct {
		name    string
		heatmap map[string]int
		want    int
	}{
		{"empty heatmap", map[string]int{}, 0},
		{"single day", map[string]int{"2026-01-01": 1}, 1},
		{"three consecutive days", map[string]int{"2026-01-01": 1, "2026-01-02": 1, "2026-01-03": 1}, 3},
		{"two separate streaks picks the longer one", map[string]int{
			"2026-01-01": 1, "2026-01-02": 1, // streak of 2
			"2026-01-10": 1, "2026-01-11": 1, "2026-01-12": 1, // streak of 3
		}, 3},
		{"zero-count days don't count as activity", map[string]int{"2026-01-01": 0, "2026-01-02": 1}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeMaxStreak(tt.heatmap))
		})
	}
}
