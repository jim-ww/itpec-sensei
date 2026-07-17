package termui

import (
	"fmt"
	"time"

	"github.com/fatih/color"
)

// PrintHeatmap renders a calendar-style ANSI color-block grid for the last
// `weeks` weeks, keyed off day ("2006-01-02") -> count.
func PrintHeatmap(counts map[string]int, weeks int) {
	today := time.Now().UTC()
	start := today.AddDate(0, 0, -7*weeks+1)
	// Align start to a Sunday.
	for start.Weekday() != time.Sunday {
		start = start.AddDate(0, 0, -1)
	}

	days := make([]time.Time, 0, weeks*7)
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}

	dayLabels := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	for row := range len(dayLabels) {
		fmt.Printf("  %-3s ", dayLabels[row])
		for col := row; col < len(days); col += 7 {
			d := days[col]
			key := d.Format("2006-01-02")
			fmt.Print(blockForCount(counts[key]))
		}
		fmt.Println()
	}
}

func blockForCount(n int) string {
	switch {
	case n == 0:
		return color.New(color.FgHiBlack).Sprint("░")
	case n < 5:
		return color.New(color.FgGreen).Sprint("▒")
	case n < 15:
		return color.New(color.FgHiGreen).Sprint("▓")
	default:
		return color.New(color.FgHiGreen, color.Bold).Sprint("█")
	}
}
