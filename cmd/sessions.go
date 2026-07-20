package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jim-ww/itpec-sensei/core"
	"github.com/jim-ww/itpec-sensei/termui"
)

// newSessionsCmd implements `itpec-sensei sessions [--scope=...] [--limit=N]`.
func newSessionsCmd(app *App) *cobra.Command {
	var scope, order string
	var limit int
	var incomplete bool
	var deleteID int64
	var yes bool

	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List past practice sessions (newest first)",
		Args:  cobra.NoArgs,
		Example: `  itpec-sensei sessions --incomplete
  itpec-sensei sessions --delete=42`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := app.Core
			if order != "newest" && order != "oldest" {
				return fmt.Errorf("invalid --order %q, expected newest or oldest", order)
			}

			if deleteID > 0 {
				return runDeleteSession(ctx, c, deleteID, yes)
			}

			var records []core.SessionRecord
			var err error
			if incomplete {
				records, err = c.IncompleteSessions(ctx, limit)
			} else {
				records, err = c.GetSessions(ctx, core.Scope(scope), core.HistoryOrder(order), limit)
			}
			if err != nil {
				return fmt.Errorf("get sessions: %w", err)
			}

			if incomplete {
				fmt.Println("resumable practice sessions")
				fmt.Println()
			} else {
				fmt.Printf("practice sessions (scope=%s, order=%s)\n\n", scope, order)
			}
			if len(records) == 0 {
				fmt.Println("No sessions recorded yet.")
				return nil
			}

			rows := make([][]string, len(records))
			for i, r := range records {
				score := "—"
				if r.Answered > 0 {
					score = fmt.Sprintf("%d%%", r.Correct*100/r.Answered)
				}
				rows[i] = []string{
					fmt.Sprintf("%d", r.ID), r.StartedAt.Local().Format("2006-01-02 15:04:05"),
					orDash(r.ExamID), r.Mode, r.OrderStrategy, score, fmt.Sprintf("%d", r.Answered), orDash(r.ExitReason),
				}
			}
			termui.PrintTable([]string{"ID", "Started At", "Exam", "Mode", "Order", "Score", "Answered", "Exit"}, rows)

			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "all", "all | exam:<id> | part:am | part:pm")
	cmd.Flags().StringVar(&order, "order", "newest", "newest | oldest")
	cmd.Flags().IntVar(&limit, "limit", 20, "max sessions to show (0 = all)")
	cmd.Flags().BoolVar(&incomplete, "incomplete", false, "only show sessions that never finished cleanly (interrupted, or killed before it could mark completion) — use to find an id for \"practice --continue\"")
	cmd.Flags().Int64Var(&deleteID, "delete", 0, "permanently delete this session and its attempts, instead of listing")
	cmd.Flags().BoolVar(&yes, "yes", false, "with --delete, skip the confirmation prompt")
	return cmd
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
