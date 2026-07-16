package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunSummary implements `itpec-trainer [--scope=...] [--period=...]`.
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
	stats, err := c.GetTopicStats(ctx, core.Scope(*scope))
	if err != nil {
		return fmt.Errorf("get topic stats: %w", err)
	}

	fmt.Printf("itpec-trainer — progress summary (scope=%s, period=%s)\n\n", *scope, *period)
	fmt.Printf("Answered:      %d\n", summary.Answered)
	fmt.Printf("Correct:       %d (%.0f%%)\n", summary.Correct, summary.Accuracy*100)
	fmt.Printf("Streak:        %d day(s)\n", summary.Streak)
	fmt.Printf("Review queue:  %d question(s)\n\n", summary.ReviewQueue)

	fmt.Println("Activity (last 12 weeks):")
	printHeatmap(summary.Heatmap)

	fmt.Println("\nPer-topic accuracy:")
	printTopicBars(stats)

	return nil
}
