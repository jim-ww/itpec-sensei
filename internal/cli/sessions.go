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
	incomplete := fs.Bool("incomplete", false, "only show sessions that never finished cleanly (interrupted, or killed before it could mark completion) — use to find an id for \"practice --continue\"")
	deleteID := fs.Int64("delete", 0, "permanently delete this session and its attempts, instead of listing")
	yes := fs.Bool("yes", false, "with --delete, skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *order != "newest" && *order != "oldest" {
		return fmt.Errorf("invalid --order %q, expected newest or oldest", *order)
	}

	if *deleteID > 0 {
		return runDeleteSession(ctx, c, *deleteID, *yes)
	}

	var records []core.SessionRecord
	var err error
	if *incomplete {
		records, err = c.IncompleteSessions(ctx, *limit)
	} else {
		records, err = c.GetSessions(ctx, core.Scope(*scope), core.HistoryOrder(*order), *limit)
	}
	if err != nil {
		return fmt.Errorf("get sessions: %w", err)
	}

	if *incomplete {
		fmt.Println("itpec-sensei — resumable practice sessions")
		fmt.Println()
	} else {
		fmt.Printf("itpec-sensei — practice sessions (scope=%s, order=%s)\n\n", *scope, *order)
	}
	if len(records) == 0 {
		fmt.Println("No sessions recorded yet.")
		return nil
	}

	fmt.Printf("%-6s  %-19s  %-16s  %-8s  %-11s  %-6s  %-9s  %s\n",
		"ID", "Started At", "Exam", "Mode", "Order", "Score", "Answered", "Exit")
	for _, r := range records {
		score := "—"
		if r.Answered > 0 {
			score = fmt.Sprintf("%d%%", r.Correct*100/r.Answered)
		}
		fmt.Printf("%-6d  %-19s  %-16s  %-8s  %-11s  %-6s  %-9d  %s\n",
			r.ID, r.StartedAt.Local().Format("2006-01-02 15:04:05"),
			orDash(r.ExamID), r.Mode, r.OrderStrategy, score, r.Answered, orDash(r.ExitReason))
	}

	return nil
}

// runDeleteSession implements `itpec-sensei sessions --delete=<id> [--yes]`.
func runDeleteSession(ctx context.Context, c *core.Core, sessionID int64, yes bool) error {
	if !yes && !confirm(fmt.Sprintf("This will permanently delete session %d and all its attempts. Continue? [y/N] ", sessionID)) {
		fmt.Println("Aborted.")
		return nil
	}
	if err := c.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	fmt.Printf("Session %d deleted.\n", sessionID)
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
