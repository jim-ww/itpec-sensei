package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunSessions implements `itpec-sensei sessions [--scope=...] [--limit=N]`.
func RunSessions(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("sessions", flag.ExitOnError)
	scope := fs.String("scope", "all", "all | exam:<id> | part:am | part:pm")
	order := fs.String("order", "newest", "newest | oldest")
	limit := fs.Int("limit", 20, "max sessions to show (0 = all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *order != "newest" && *order != "oldest" {
		return fmt.Errorf("invalid --order %q, expected newest or oldest", *order)
	}

	records, err := c.GetSessions(ctx, core.Scope(*scope), core.HistoryOrder(*order), *limit)
	if err != nil {
		return fmt.Errorf("get sessions: %w", err)
	}

	fmt.Printf("itpec-sensei — practice sessions (scope=%s, order=%s)\n\n", *scope, *order)
	if len(records) == 0 {
		fmt.Println("No sessions recorded yet.")
		return nil
	}

	fmt.Printf("%-19s  %-16s  %-8s  %-11s  %-6s  %-9s  %s\n",
		"Started At", "Exam", "Mode", "Order", "Score", "Answered", "Exit")
	for _, r := range records {
		score := "—"
		if r.Answered > 0 {
			score = fmt.Sprintf("%d%%", r.Correct*100/r.Answered)
		}
		fmt.Printf("%-19s  %-16s  %-8s  %-11s  %-6s  %-9d  %s\n",
			r.StartedAt.Local().Format("2006-01-02 15:04:05"),
			orDash(r.ExamID), r.Mode, r.OrderStrategy, score, r.Answered, orDash(r.ExitReason))
	}

	return nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
