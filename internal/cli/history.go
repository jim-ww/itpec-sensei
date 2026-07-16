package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunHistory implements `itpec-sensei history [--scope=...] [--limit=N]`.
func RunHistory(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	scope := fs.String("scope", "all", "all | topic:<name> | exam:<id> | part:am | part:pm")
	order := fs.String("order", "newest", "newest | oldest")
	limit := fs.Int("limit", 20, "max attempts to show (0 = all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *order != "newest" && *order != "oldest" {
		return fmt.Errorf("invalid --order %q, expected newest or oldest", *order)
	}

	records, err := c.GetHistory(ctx, core.Scope(*scope), core.HistoryOrder(*order), *limit)
	if err != nil {
		return fmt.Errorf("get history: %w", err)
	}

	fmt.Printf("itpec-sensei — attempt history (scope=%s, order=%s)\n\n", *scope, *order)
	if len(records) == 0 {
		fmt.Println("No attempts recorded yet.")
		return nil
	}

	fmt.Printf("%-19s  %-16s  %-22s  %-6s  %-7s  %s\n", "Answered At", "Exam", "Topic", "Answer", "Result", "Question")
	for _, r := range records {
		result := "correct"
		if !r.Correct {
			result = "wrong"
		}
		if r.TimedOut {
			result += "*"
		}
		fmt.Printf("%-19s  %-16s  %-22s  %-6s  %-7s  %s\n",
			r.AnsweredAt.Local().Format("2006-01-02 15:04:05"),
			r.ExamID, r.Topic, r.Answer, result, r.QuestionID)
	}
	fmt.Println("\n(* = answered after the per-question time limit)")

	return nil
}
