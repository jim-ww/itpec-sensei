package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

const barWidth = 30

// printTopicBars renders a simple proportional bar chart of per-topic accuracy.
func printTopicBars(stats []core.TopicStat) {
	if len(stats) == 0 {
		fmt.Println("  (no attempts yet)")
		return
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Topic < stats[j].Topic })

	maxLen := 0
	for _, s := range stats {
		if len(s.Topic) > maxLen {
			maxLen = len(s.Topic)
		}
	}

	for _, s := range stats {
		filled := int(s.Accuracy * barWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		label := fmt.Sprintf("%-*s", maxLen, s.Topic)
		pct := fmt.Sprintf("%3.0f%%", s.Accuracy*100)
		barColor := color.New(color.FgGreen)
		if s.Accuracy < 0.5 {
			barColor = color.New(color.FgRed)
		} else if s.Accuracy < 0.8 {
			barColor = color.New(color.FgYellow)
		}
		fmt.Printf("  %s  %s  %s  (%d/%d)\n", label, barColor.Sprint(bar), pct, s.Correct, s.Answered)
	}
}
