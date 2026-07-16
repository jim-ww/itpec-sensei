package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunSummary implements `itpec-sensei [--scope=...] [--period=...]`.
func RunSummary(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	scope := fs.String("scope", "all", "all | topic:<name> | exam:<id> | part:am | part:pm")
	period := fs.String("period", "all", "week | month | all")
	if err := fs.Parse(args); err != nil {
		return err
	}

	summary, err := c.GetProgressSummary(ctx, core.Scope(*scope), core.Period(*period))
	if err != nil {
		return fmt.Errorf("get progress summary: %w", err)
	}
	topicStats, err := c.GetTopicStats(ctx, core.Scope(*scope))
	if err != nil {
		return fmt.Errorf("get topic stats: %w", err)
	}
	examStats, err := c.GetExamStats(ctx, core.Scope(*scope))
	if err != nil {
		return fmt.Errorf("get exam stats: %w", err)
	}

	fmt.Printf("itpec-sensei — progress summary (scope=%s, period=%s)\n\n", *scope, *period)
	fmt.Printf("Answered:      %d\n", summary.Answered)
	fmt.Printf("Streak:        %d day(s) (best: %d)\n", summary.Streak, summary.MaxStreak)
	fmt.Printf("Review queue:  %d question(s) — most recent attempt was wrong, due for another pass\n", summary.ReviewQueue)
	fmt.Println()

	fmt.Println("By part (AM and PM test different material, so they're never blended):")
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
	printHeatmap(summary.Heatmap)

	fmt.Println("\nPer-topic accuracy:")
	printTopicBars(topicStats)

	fmt.Println("\nPer-exam accuracy:")
	printExamBars(examStats)

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
