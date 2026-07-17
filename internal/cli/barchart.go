package cli

import (
	"fmt"
	"sort"

	"github.com/jim-ww/itpec-sensei/internal/core"
	"github.com/jim-ww/itpec-sensei/pkg/termui"
)

// printTopicBars renders a proportional bar chart of per-topic accuracy.
func printTopicBars(stats []core.TopicStat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].Topic < stats[j].Topic })
	items := make([]termui.BarItem, len(stats))
	for i, s := range stats {
		items[i] = termui.BarItem{Label: s.Topic, Fraction: s.Accuracy, Detail: countDetail(s.Correct, s.Answered)}
	}
	termui.PrintBars(items, "  (no attempts yet)")
}

// printExamBars renders a proportional bar chart of per-exam accuracy.
func printExamBars(stats []core.ExamStat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].ExamID < stats[j].ExamID })
	items := make([]termui.BarItem, len(stats))
	for i, s := range stats {
		items[i] = termui.BarItem{Label: s.ExamID, Fraction: s.Accuracy, Detail: countDetail(s.Correct, s.Answered)}
	}
	termui.PrintBars(items, "  (no attempts yet)")
}

func countDetail(correct, answered int) string {
	return fmt.Sprintf("(%d/%d)", correct, answered)
}
