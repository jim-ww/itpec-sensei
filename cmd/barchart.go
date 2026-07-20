package cmd

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/jim-ww/itpec-sensei/core"
	"github.com/jim-ww/itpec-sensei/termui"
)

// printTopicBars renders a proportional bar chart of per-topic accuracy.
func printTopicBars(stats []core.TopicStat) {
	slices.SortFunc(stats, func(a, b core.TopicStat) int { return cmp.Compare(a.Topic, b.Topic) })
	items := make([]termui.BarItem, len(stats))
	for i, s := range stats {
		items[i] = termui.BarItem{Label: s.Topic, Fraction: s.Accuracy, Detail: countDetail(s.Correct, s.Answered)}
	}
	termui.PrintBars(items, "  (no attempts yet)")
}

// printExamBars renders a proportional bar chart of per-exam accuracy.
func printExamBars(stats []core.ExamStat) {
	slices.SortFunc(stats, func(a, b core.ExamStat) int { return cmp.Compare(a.ExamID, b.ExamID) })
	items := make([]termui.BarItem, len(stats))
	for i, s := range stats {
		items[i] = termui.BarItem{Label: s.ExamID, Fraction: s.Accuracy, Detail: countDetail(s.Correct, s.Answered)}
	}
	termui.PrintBars(items, "  (no attempts yet)")
}

func countDetail(correct, answered int) string {
	return fmt.Sprintf("(%d/%d)", correct, answered)
}
