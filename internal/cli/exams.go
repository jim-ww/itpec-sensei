package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunExams implements `itpec-sensei exams`, listing all known exam IDs.
func RunExams(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("exams", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	exams, err := c.ListExams(ctx)
	if err != nil {
		return fmt.Errorf("list exams: %w", err)
	}

	fmt.Println("itpec-sensei — exams")
	for _, id := range exams {
		fmt.Println(" ", id)
	}
	return nil
}

// RunExam implements `itpec-sensei exam <examID>`, printing readable
// metadata + the user's own progress on that exam.
func RunExam(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("exam", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: itpec-sensei exam <examID>")
	}
	examID := fs.Arg(0)

	detail, err := c.GetExam(ctx, examID)
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
}
