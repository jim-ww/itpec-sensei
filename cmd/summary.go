package cmd

import (
	"context"
	"fmt"

	"github.com/jim-ww/itpec-sensei/core"
	"github.com/jim-ww/itpec-sensei/termui"
)

// runSummary implements the root command's default action:
// `itpec-sensei [--topic=...] [--tags=...] [--exam=...] [--part=...] [--period=...] [--weakest-tags=N]`.
func runSummary(ctx context.Context, c *core.Core, scope core.ScopeFilter, period core.Period, weakestTagsLimit int) error {
	summary, err := c.GetProgressSummary(ctx, scope, period)
	if err != nil {
		return fmt.Errorf("get progress summary: %w", err)
	}
	topicStats, err := c.GetTopicStats(ctx, scope)
	if err != nil {
		return fmt.Errorf("get topic stats: %w", err)
	}
	examStats, err := c.GetExamStats(ctx, scope)
	if err != nil {
		return fmt.Errorf("get exam stats: %w", err)
	}
	var weakestTags []core.TagStat
	if weakestTagsLimit != 0 {
		tagStats, err := c.GetTagStats(ctx, scope)
		if err != nil {
			return fmt.Errorf("get tag stats: %w", err)
		}
		weakestTags = core.WeakestTags(tagStats, weakestTagsLimit, core.MinTagAttempts)
	}

	fmt.Printf("itpec-sensei — progress summary (scope=%s, period=%s)\n\n", scopeLabel(scope), period)
	fmt.Printf("Answered:      %d\n", summary.Answered)
	fmt.Printf("Streak:        %d day(s) (best: %d)\n", summary.Streak, summary.MaxStreak)
	fmt.Printf("Review queue:  %d question(s)\n", summary.ReviewQueue)
	fmt.Println()

	fmt.Println("By part:")
	if len(summary.PartStats) == 0 {
		fmt.Println("  (no attempts yet)")
	}
	for _, p := range summary.PartStats {
		line := fmt.Sprintf("  %-6s %d/%d (%.0f%%)", partLabel(p.Part), p.Correct, p.Answered, p.Accuracy*100)
		if p.AvgTimeMs > 0 {
			line += fmt.Sprintf("  avg %.1fs, median %.1fs", p.AvgTimeMs/1000, p.MedianTimeMs/1000)
		}
		fmt.Println(line)
	}

	fmt.Println("\nActivity (last 12 weeks):")
	termui.PrintHeatmap(summary.Heatmap, 12)

	fmt.Println("\nPer-topic accuracy:")
	printTopicBars(topicStats)

	fmt.Println("\nPer-exam accuracy:")
	printExamBars(examStats)

	if weakestTagsLimit != 0 {
		fmt.Printf("\nWeakest tags (min. %d attempts", core.MinTagAttempts)
		if weakestTagsLimit > 0 {
			fmt.Printf(", top %d", weakestTagsLimit)
		}
		fmt.Println("):")
		printTagBars(weakestTags)
	}

	return nil
}

func partLabel(part string) string {
	switch part {
	case "am":
		return "AM"
	case "pm":
		return "PM"
	default:
		return "Other"
	}
}
