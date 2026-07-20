package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newExamsCmd implements `itpec-sensei exams`, listing all known exam IDs.
func newExamsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "exams",
		Short: "List known exam IDs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exams, err := app.Core.ListExams(cmd.Context())
			if err != nil {
				return fmt.Errorf("list exams: %w", err)
			}
			fmt.Println("itpec-sensei — exams")
			for _, id := range exams {
				fmt.Println(" ", id)
			}
			return nil
		},
	}
}

// newExamCmd implements `itpec-sensei exam <examID>`, printing readable
// metadata + the user's own progress on that exam.
func newExamCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "exam <examID>",
		Short:   "Show readable metadata + your progress for one exam",
		Args:    cobra.ExactArgs(1),
		Example: "  itpec-sensei exam 2025A_FE-A",
		RunE: func(cmd *cobra.Command, args []string) error {
			examID := args[0]
			detail, err := app.Core.GetExam(cmd.Context(), examID)
			if err != nil {
				return fmt.Errorf("get exam: %w", err)
			}

			fmt.Printf("%s — %s\n", detail.ExamID, orDash(detail.Name))
			fmt.Printf("Date:            %s\n", orDash(detail.Date))
			fmt.Printf("Part:            %s\n", orDash(partLabel(detail.Part)))
			fmt.Printf("Duration:        %d min\n", detail.DurationMinutes)
			fmt.Printf("Questions:       %d\n", detail.TotalQuestions)
			if detail.TargetSecondsPerQuestion > 0 {
				fmt.Printf("Pacing target:   %ds/question (real exam time pressure)\n", detail.TargetSecondsPerQuestion)
			}
			if detail.Answered > 0 {
				fmt.Printf("Your progress:   %d/%d answered, %.0f%% correct\n", detail.Answered, detail.TotalQuestions, detail.Accuracy*100)
				if detail.AvgTimeMs > 0 {
					verdict := "on pace"
					if detail.TargetSecondsPerQuestion > 0 && detail.AvgTimeMs/1000 > float64(detail.TargetSecondsPerQuestion) {
						verdict = "slower than target"
					}
					fmt.Printf("Your avg time:   %.1fs/question (%s)\n", detail.AvgTimeMs/1000, verdict)
				}
			} else {
				fmt.Println("Your progress:   not attempted yet")
			}
			return nil
		},
	}
}
