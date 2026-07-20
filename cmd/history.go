package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jim-ww/itpec-sensei/core"
	"github.com/jim-ww/itpec-sensei/termui"
)

// newHistoryCmd implements `itpec-sensei history [--scope=...] [--limit=N]`
// and, via --undo, `itpec-sensei history --undo [--session=N]`.
func newHistoryCmd(app *App) *cobra.Command {
	var scope, order string
	var limit int
	var undo bool
	var session int64

	cmd := &cobra.Command{
		Use:   "history",
		Short: "List past attempts (newest first)",
		Args:  cobra.NoArgs,
		Example: `  itpec-sensei history
  itpec-sensei history --undo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := app.Core
			if order != "newest" && order != "oldest" {
				return fmt.Errorf("invalid --order %q, expected newest or oldest", order)
			}

			if undo {
				r, err := c.UndoLastAnswer(ctx, session)
				if err != nil {
					return fmt.Errorf("undo last answer: %w", err)
				}
				result := "correct"
				if !r.Correct {
					result = "wrong"
				}
				fmt.Printf("Undone: %s (%s), answer %q, %s, answered %s\n",
					r.QuestionID, r.Topic, r.Answer, result, r.AnsweredAt.Local().Format("2006-01-02 15:04:05"))
				return nil
			}

			records, err := c.GetHistory(ctx, core.Scope(scope), core.HistoryOrder(order), limit)
			if err != nil {
				return fmt.Errorf("get history: %w", err)
			}

			fmt.Printf("itpec-sensei — attempt history (scope=%s, order=%s)\n\n", scope, order)
			if len(records) == 0 {
				fmt.Println("No attempts recorded yet.")
				return nil
			}

			rows := make([][]string, len(records))
			for i, r := range records {
				result := "correct"
				if !r.Correct {
					result = "wrong"
				}
				if r.TimedOut {
					result += "*"
				}
				rows[i] = []string{
					r.AnsweredAt.Local().Format("2006-01-02 15:04:05"),
					r.ExamID, r.Topic, r.Answer, result, r.QuestionID,
				}
			}
			termui.PrintTable([]string{"Answered At", "Exam", "Topic", "Answer", "Result", "Question"}, rows)
			fmt.Println("\n(* = answered after the per-question time limit)")

			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "all", "all | topic:<name> | tag:<name> | exam:<id> | part:am | part:pm")
	cmd.Flags().StringVar(&order, "order", "newest", "newest | oldest")
	cmd.Flags().IntVar(&limit, "limit", 20, "max attempts to show (0 = all)")
	cmd.Flags().BoolVar(&undo, "undo", false, "delete the most recent attempt instead of listing history")
	cmd.Flags().Int64Var(&session, "session", 0, "with --undo, only consider this session id (default 0 = most recent across all sessions)")
	return cmd
}
