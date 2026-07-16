package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

const barWidth = 30

// barItem is one labeled row in a proportional accuracy bar chart.
type barItem struct {
	Label    string
	Correct  int
	Answered int
	Accuracy float64
}

// printBars renders a simple proportional bar chart of accuracy per item.
func printBars(items []barItem) {
	if len(items) == 0 {
		fmt.Println("  (no attempts yet)")
		return
	}

	maxLen := 0
	for _, it := range items {
		if len(it.Label) > maxLen {
			maxLen = len(it.Label)
		}
	}

	for _, it := range items {
		filled := int(it.Accuracy * barWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		label := fmt.Sprintf("%-*s", maxLen, it.Label)
		pct := fmt.Sprintf("%3.0f%%", it.Accuracy*100)
		barColor := color.New(color.FgGreen)
		if it.Accuracy < 0.5 {
			barColor = color.New(color.FgRed)
		} else if it.Accuracy < 0.8 {
			barColor = color.New(color.FgYellow)
		}
		fmt.Printf("  %s  %s  %s  (%d/%d)\n", label, barColor.Sprint(bar), pct, it.Correct, it.Answered)
	}
}

// printTopicBars renders a proportional bar chart of per-topic accuracy.
func printTopicBars(stats []core.TopicStat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].Topic < stats[j].Topic })
	items := make([]barItem, len(stats))
	for i, s := range stats {
		items[i] = barItem{Label: s.Topic, Correct: s.Correct, Answered: s.Answered, Accuracy: s.Accuracy}
	}
	printBars(items)
}

// printExamBars renders a proportional bar chart of per-exam accuracy.
func printExamBars(stats []core.ExamStat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].ExamID < stats[j].ExamID })
	items := make([]barItem, len(stats))
	for i, s := range stats {
		items[i] = barItem{Label: s.ExamID, Correct: s.Correct, Answered: s.Answered, Accuracy: s.Accuracy}
	}
	printBars(items)
}
