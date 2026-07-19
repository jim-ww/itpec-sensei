package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

type practiceFlags struct {
	examType          string
	examID            string
	part              string
	topic             string
	question          int
	limit             int
	mode              string
	order             string
	timeLimit         time.Duration
	questionTimeLimit time.Duration
	imageViewer       string
	showAnswer        bool
	dark              bool
}

// poolFlagNames are the flags that plan a fresh question pool — they're
// meaningless (and rejected) alongside --continue/--repeat, which instead
// reuse an existing session's params.
var poolFlagNames = []string{"exam-type", "exam", "part", "topic", "q", "limit", "mode", "order", "time-limit", "question-time-limit"}

// newPracticeCmd implements `itpec-sensei practice ...`.
func newPracticeCmd(app *App) *cobra.Command {
	var examType, examID, part, topic, mode, order, imageViewer string
	var question, limit int
	var timeLimit, questionTimeLimit time.Duration
	var showAnswer, dark bool
	var continueID, repeatID int64

	cmd := &cobra.Command{
		Use:   "practice",
		Short: "Answer practice questions",
		Args:  cobra.NoArgs,
		Example: `  itpec-sensei practice --exam=2025A_FE-A
  itpec-sensei practice --exam-type=fe --part=pm --mode=review
  itpec-sensei practice --exam=2025A_FE-A --time-limit=150m --question-time-limit=90s
  itpec-sensei practice --exam=2025A_FE-A --q=34
  itpec-sensei practice --exam=2025A_FE-A --limit=5
  itpec-sensei practice --exam=2025A_FE-A --q=34 --answer
  itpec-sensei practice --topic="Networks" --part=am
  itpec-sensei practice --continue
  itpec-sensei practice --continue=42
  itpec-sensei practice --repeat=42`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := app.Core

			switch imageViewer {
			case "sixel", "xdg-open":
			default:
				return fmt.Errorf("invalid --image-viewer %q, expected sixel or xdg-open", imageViewer)
			}

			continueSet := cmd.Flags().Changed("continue")
			repeatSet := cmd.Flags().Changed("repeat")
			if continueSet || repeatSet {
				var conflicting []string
				for _, name := range poolFlagNames {
					if cmd.Flags().Changed(name) {
						conflicting = append(conflicting, "--"+name)
					}
				}
				if len(conflicting) > 0 {
					return fmt.Errorf("--continue/--repeat reuse the session's original params; don't combine with %s", strings.Join(conflicting, ", "))
				}
				if continueSet {
					id, err := resolveContinueSessionID(ctx, c, continueID)
					if err != nil {
						return err
					}
					return runContinueSession(ctx, c, id, imageViewer, showAnswer, dark)
				}
				return runRepeatSession(ctx, c, repeatID, imageViewer, showAnswer, dark)
			}

			if question > 0 && examID == "" {
				return fmt.Errorf("-q requires --exam")
			}

			if topic != "" && !contains(c.Bank.Topics(), topic) {
				return fmt.Errorf("invalid --topic %q; known topics are: %s", topic, strings.Join(c.Bank.Topics(), ", "))
			}

			partVal := strings.ToLower(part)
			switch partVal {
			case "am", "pm":
				// valid
			case "all", "":
				partVal = ""
			default:
				return fmt.Errorf("invalid --part %q, expected am, pm, or all", part)
			}

			pf := practiceFlags{
				examType:          examType,
				examID:            examID,
				part:              partVal,
				topic:             topic,
				question:          question,
				limit:             limit,
				mode:              mode,
				order:             order,
				timeLimit:         timeLimit,
				questionTimeLimit: questionTimeLimit,
				imageViewer:       imageViewer,
				showAnswer:        showAnswer,
				dark:              dark,
			}
			return runPracticeSession(ctx, c, pf)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&examType, "exam-type", "fe", "fe | itpassport")
	flags.StringVar(&examID, "exam", "", "scope to one exam id")
	flags.StringVar(&part, "part", "all", "am | pm | all — which exam session to practice (e.g. FE-AM/FE-A vs FE-PM/FE-B); ignored if --exam is set")
	flags.StringVar(&topic, "topic", "", "filter to one topic; combines with --exam/--part (see \"itpec-sensei topics\" for valid names)")
	flags.IntVar(&question, "q", 0, "practice only this specific question number within --exam")
	flags.IntVar(&limit, "limit", 0, "max number of questions this session (0 = no limit)")
	flags.StringVar(&mode, "mode", "normal", "normal | review (spaced repetition: only questions due under the Leitner-box schedule)")
	flags.StringVar(&order, "order", "random", "sequential | random | fail-count | weak (weighted towards low-accuracy topics)")
	flags.DurationVar(&timeLimit, "time-limit", 0, "whole-session time limit, e.g. 150m")
	flags.DurationVar(&questionTimeLimit, "question-time-limit", 0, "per-question time limit, e.g. 90s")
	flags.StringVar(&imageViewer, "image-viewer", "sixel", "sixel | xdg-open — how to display question images")
	flags.BoolVar(&showAnswer, "answer", false, "reveal the correct answer/explanation immediately per question instead of grading input; no DB writes in this mode")
	flags.BoolVar(&dark, "dark", true, "invert question image colors, for dark terminal themes (default on; pass --dark=false to see original colors)")
	flags.Int64Var(&continueID, "continue", 0, "resume a not-completed session exactly where it left off: bare --continue resumes the most recent not-completed session, or pass --continue=<id> for a specific one (see \"itpec-sensei sessions --incomplete\")")
	flags.Lookup("continue").NoOptDefVal = "0" // makes a bare --continue (no =value) valid, same trick bool flags use
	flags.Int64Var(&repeatID, "repeat", 0, "start a new session reusing exam/topic/part/mode/order/limits from an existing session (completed or not), with a fresh question draw")
	cmd.MarkFlagsMutuallyExclusive("continue", "repeat")

	return cmd
}

// resolveContinueSessionID returns explicitID if given (>0), otherwise looks
// up the most recent not-completed session — this backs bare `--continue`
// (no id) auto-resuming whatever was last left incomplete.
func resolveContinueSessionID(ctx context.Context, c *core.Core, explicitID int64) (int64, error) {
	if explicitID > 0 {
		return explicitID, nil
	}
	incomplete, err := c.IncompleteSessions(ctx, 1)
	if err != nil {
		return 0, err
	}
	if len(incomplete) == 0 {
		return 0, fmt.Errorf("no incomplete session to continue")
	}
	return incomplete[0].ID, nil
}

// runContinueSession resumes a not-completed session exactly where it left
// off: the same session row (no new StartSession call), same filters and
// order strategy re-run against current data, minus whatever was already
// answered in it. Nothing about the original pool is persisted — it's
// recomputed here (see planPool) rather than read back from storage.
func runContinueSession(ctx context.Context, c *core.Core, sessionID int64, imageViewer string, showAnswer, dark bool) error {
	incomplete, err := c.IncompleteSessions(ctx, 0)
	if err != nil {
		return err
	}
	found := false
	for _, s := range incomplete {
		if s.ID == sessionID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("session %d is not resumable (already completed, or doesn't exist) — see \"itpec-sensei sessions --incomplete\"", sessionID)
	}

	params, err := c.GetSessionParams(ctx, sessionID)
	if err != nil {
		return err
	}
	answered, err := c.AnsweredQuestionIDs(ctx, sessionID)
	if err != nil {
		return err
	}

	pf := practiceFlagsFromParams(params, imageViewer, showAnswer, dark)
	pool, err := planPool(ctx, c, pf)
	if err != nil {
		return err
	}

	var remaining []*core.Question
	for _, q := range pool {
		if !answered[q.GlobalID()] {
			remaining = append(remaining, q)
		}
	}
	if pf.limit > 0 {
		budget := pf.limit - len(answered)
		if budget < 0 {
			budget = 0
		}
		if budget < len(remaining) {
			remaining = remaining[:budget]
		}
	}
	if len(remaining) == 0 {
		fmt.Println("No remaining questions in this session — marking it completed.")
		return c.EndSession(ctx, sessionID, "completed")
	}

	fmt.Printf("Continuing session %d — %d question(s) remaining.\n", sessionID, len(remaining))
	return executeSession(ctx, c, pf, remaining, sessionID)
}

// runRepeatSession starts a brand-new session reusing another session's
// filter params (exam/topic/part/mode/order/limits), with a fresh draw —
// unlike --continue, the source session need not be incomplete.
func runRepeatSession(ctx context.Context, c *core.Core, sessionID int64, imageViewer string, showAnswer, dark bool) error {
	params, err := c.GetSessionParams(ctx, sessionID)
	if err != nil {
		return err
	}
	pf := practiceFlagsFromParams(params, imageViewer, showAnswer, dark)
	return runPracticeSession(ctx, c, pf)
}

func practiceFlagsFromParams(p core.SessionParams, imageViewer string, showAnswer, dark bool) practiceFlags {
	pf := practiceFlags{
		examType:    p.ExamType,
		examID:      p.ExamID,
		part:        p.Part,
		topic:       p.Topic,
		question:    p.QuestionNumber,
		limit:       p.QuestionLimit,
		mode:        p.Mode,
		order:       p.OrderStrategy,
		imageViewer: imageViewer,
		showAnswer:  showAnswer,
		dark:        dark,
	}
	if p.TimeLimitSeconds != nil {
		pf.timeLimit = time.Duration(*p.TimeLimitSeconds) * time.Second
	}
	if p.QuestionTimeLimitSeconds != nil {
		pf.questionTimeLimit = time.Duration(*p.QuestionTimeLimitSeconds) * time.Second
	}
	return pf
}

func runPracticeSession(ctx context.Context, c *core.Core, pf practiceFlags) error {
	ordered, err := planQuestions(ctx, c, pf)
	if err != nil {
		return err
	}
	if len(ordered) == 0 {
		fmt.Println("No questions match this filter.")
		return nil
	}
	return executeSession(ctx, c, pf, ordered, 0)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
