package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// optionalSessionID backs --continue: usable bare as `--continue` (resume the
// most recent not-completed session) or with an explicit id as
// `--continue=42`. It implements the flag.Value + IsBoolFlag interfaces so
// the flag package treats a bare `--continue` as "set with no value" rather
// than demanding a following argument, the same trick bool flags use.
type optionalSessionID struct {
	set   bool
	value int64
}

func (o *optionalSessionID) String() string {
	if o == nil || !o.set {
		return ""
	}
	return strconv.FormatInt(o.value, 10)
}

func (o *optionalSessionID) Set(s string) error {
	if s == "" || s == "true" {
		o.set, o.value = true, 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid session id %q", s)
	}
	o.set, o.value = true, v
	return nil
}

func (o *optionalSessionID) IsBoolFlag() bool { return true }

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

// RunPractice implements `itpec-sensei practice ...`.
func RunPractice(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("practice", flag.ExitOnError)
	examType := fs.String("exam-type", "fe", "fe | itpassport")
	examID := fs.String("exam", "", "scope to one exam id")
	part := fs.String("part", "all", "am | pm | all — which exam session to practice (e.g. FE-AM/FE-A vs FE-PM/FE-B); ignored if --exam is set")
	topic := fs.String("topic", "", "filter to one topic; combines with --exam/--part (see \"itpec-sensei topics\" for valid names)")
	question := fs.Int("q", 0, "practice only this specific question number within --exam")
	limit := fs.Int("limit", 0, "max number of questions this session (0 = no limit)")
	mode := fs.String("mode", "normal", "normal | review")
	order := fs.String("order", "random", "sequential | random | fail-count | fail-rate | weak (weighted towards low-accuracy topics)")
	timeLimit := fs.Duration("time-limit", 0, "whole-session time limit, e.g. 150m")
	questionTimeLimit := fs.Duration("question-time-limit", 0, "per-question time limit, e.g. 90s")
	imageViewer := fs.String("image-viewer", "sixel", "sixel | xdg-open — how to display question images")
	showAnswer := fs.Bool("answer", false, "reveal the correct answer/explanation immediately per question instead of grading input; no DB writes in this mode")
	dark := fs.Bool("dark", true, "invert question image colors, for dark terminal themes (default on; pass --dark=false to see original colors)")
	var continueFlag optionalSessionID
	fs.Var(&continueFlag, "continue", "resume a not-completed session exactly where it left off: bare `--continue` resumes the most recent not-completed session, or pass `--continue=<id>` for a specific one (see \"itpec-sensei sessions --incomplete\")")
	repeatID := fs.Int64("repeat", 0, "start a new session reusing exam/topic/part/mode/order/limits from an existing session (completed or not), with a fresh question draw")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch *imageViewer {
	case "sixel", "xdg-open":
	default:
		return fmt.Errorf("invalid --image-viewer %q, expected sixel or xdg-open", *imageViewer)
	}

	if continueFlag.set && *repeatID > 0 {
		return fmt.Errorf("--continue and --repeat are mutually exclusive")
	}
	if continueFlag.set || *repeatID > 0 {
		var conflicting []string
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "exam-type", "exam", "part", "topic", "q", "limit", "mode", "order", "time-limit", "question-time-limit":
				conflicting = append(conflicting, "--"+f.Name)
			}
		})
		if len(conflicting) > 0 {
			return fmt.Errorf("--continue/--repeat reuse the session's original params; don't combine with %s", strings.Join(conflicting, ", "))
		}
		if continueFlag.set {
			id, err := resolveContinueSessionID(ctx, c, continueFlag.value)
			if err != nil {
				return err
			}
			return runContinueSession(ctx, c, id, *imageViewer, *showAnswer, *dark)
		}
		return runRepeatSession(ctx, c, *repeatID, *imageViewer, *showAnswer, *dark)
	}

	if *question > 0 && *examID == "" {
		return fmt.Errorf("-q requires --exam")
	}

	if *topic != "" && !contains(c.Bank.Topics(), *topic) {
		return fmt.Errorf("invalid --topic %q; known topics are: %s", *topic, strings.Join(c.Bank.Topics(), ", "))
	}

	partVal := strings.ToLower(*part)
	switch partVal {
	case "am", "pm":
		// valid
	case "all", "":
		partVal = ""
	default:
		return fmt.Errorf("invalid --part %q, expected am, pm, or all", *part)
	}

	pf := practiceFlags{
		examType:          *examType,
		examID:            *examID,
		part:              partVal,
		topic:             *topic,
		question:          *question,
		limit:             *limit,
		mode:              *mode,
		order:             *order,
		timeLimit:         *timeLimit,
		questionTimeLimit: *questionTimeLimit,
		imageViewer:       *imageViewer,
		showAnswer:        *showAnswer,
		dark:              *dark,
	}
	return runPracticeSession(ctx, c, pf)
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
// off: the same session row (no new StartSession call), the same planned
// question order, minus whatever was already answered in it.
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

	var remaining []*core.Question
	for _, gid := range params.PlannedQuestions {
		if answered[gid] {
			continue
		}
		if q := c.Bank.Question(gid); q != nil {
			remaining = append(remaining, q)
		}
	}
	if len(remaining) == 0 {
		fmt.Println("No remaining questions in this session — marking it completed.")
		return c.EndSession(ctx, sessionID, "completed")
	}

	pf := practiceFlagsFromParams(params, imageViewer, showAnswer, dark)
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
