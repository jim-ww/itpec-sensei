package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	sixel "github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
	"golang.org/x/sys/unix"

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

// planQuestions builds the ordered, limit-applied question pool for a fresh
// session, per pf's filters.
func planQuestions(ctx context.Context, c *core.Core, pf practiceFlags) ([]*core.Question, error) {
	var planned []*core.Question
	switch {
	case pf.question > 0:
		q := c.Bank.QuestionByExamAndNumber(pf.examID, pf.question)
		if q == nil {
			return nil, fmt.Errorf("question %s#%d not found", pf.examID, pf.question)
		}
		planned = []*core.Question{q}
	case pf.examID != "":
		planned = c.Bank.Questions("", pf.examID)
	default:
		planned = c.Bank.QuestionsForExams(c.Bank.ExamsByPart(pf.part))
	}
	if pf.topic != "" {
		planned = filterByTopic(planned, pf.topic)
	}
	if pf.mode == "review" && pf.question == 0 {
		var err error
		planned, err = reviewFiltered(ctx, c, planned)
		if err != nil {
			return nil, err
		}
	}
	if len(planned) == 0 {
		return nil, nil
	}

	ordered, err := orderQuestions(ctx, c, planned, pf.order)
	if err != nil {
		return nil, err
	}

	if pf.limit > 0 && pf.limit < len(ordered) {
		ordered = ordered[:pf.limit]
	}
	return ordered, nil
}

// executeSession runs the answer loop over ordered. If existingSessionID is 0,
// a new sessions row (with ordered's global IDs as planned_questions) is
// created lazily on the first submitted answer, as before; otherwise the
// caller is resuming an already-started session (--continue), so that id is
// used directly and no new row is created.
func executeSession(ctx context.Context, c *core.Core, pf practiceFlags, ordered []*core.Question, existingSessionID int64) error {
	if pf.showAnswer {
		return runAnswerReveal(c, pf, ordered)
	}

	var timeLimitSec, qTimeLimitSec *int
	if pf.timeLimit > 0 {
		v := int(pf.timeLimit.Seconds())
		timeLimitSec = &v
	}
	if pf.questionTimeLimit > 0 {
		v := int(pf.questionTimeLimit.Seconds())
		qTimeLimitSec = &v
	}

	sessionID := existingSessionID
	sessionStarted := existingSessionID > 0

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	sessionDeadline := time.Time{}
	if pf.timeLimit > 0 {
		sessionDeadline = time.Now().Add(pf.timeLimit)
	}

	answered, correct, onTime, timedOut := 0, 0, 0, 0
	exitReason := "completed"
	stdin := bufio.NewReader(os.Stdin)

	var lastExternalImage string
	defer killExternalViewer(&lastExternalImage)

questionLoop:
	for i, q := range ordered {
		fmt.Printf("\nQuestion %d of %d  (%s, q%d)  [%s %s]\n",
			i+1, len(ordered), q.ExamID, q.ID,
			color.New(color.FgGreen, color.Bold).Sprintf("%d✓", correct),
			color.New(color.FgRed, color.Bold).Sprintf("%d✗", answered-correct))
		killExternalViewer(&lastExternalImage)
		if err := renderImage(c, q, pf.imageViewer, pf.dark, &lastExternalImage); err != nil {
			fmt.Printf("[image unavailable: %v]\n", err)
		}

		answerCh := make(chan string, 1)
		go func() {
			for {
				fmt.Print("Your answer ('q' to quit, '?' if you don't know): ")
				line, _ := stdin.ReadString('\n')
				a := strings.TrimSpace(line)
				if a == "" {
					continue
				}
				answerCh <- a
				return
			}
		}()

		var timers []<-chan time.Time
		if pf.questionTimeLimit > 0 {
			timers = append(timers, time.After(pf.questionTimeLimit))
		}
		if !sessionDeadline.IsZero() {
			timers = append(timers, time.After(time.Until(sessionDeadline)))
		}

		start := time.Now()
		lateFired := false

		var answer string
	waitLoop:
		for {
			var timerCh <-chan time.Time
			if len(timers) > 0 {
				timerCh = timers[0]
			}
			select {
			case a := <-answerCh:
				answer = a
				break waitLoop
			case <-sigCh:
				exitReason = "interrupted"
				break questionLoop
			case <-timerCh:
				if !lateFired {
					lateFired = true
					fmt.Println("\n[time's up — answer will be recorded as timed out once submitted]")
				}
				timers = timers[1:]
				if len(timers) == 0 {
					// keep waiting on answerCh only
					timerCh = nil
				}
			}
		}

		if strings.EqualFold(answer, "q") {
			exitReason = "interrupted"
			break questionLoop
		}

		dontKnow := answer == "?" || strings.EqualFold(answer, "idk")
		if dontKnow {
			// Recorded as its own sentinel answer so it's distinguishable from a
			// wrong guess in the attempts log, but still grades as incorrect and
			// still counts as an answered question (the user did respond).
			answer = "idk"
		}

		if !sessionStarted {
			id, err := c.StartSession(ctx, core.SessionParams{
				ExamType:                 pf.examType,
				ExamID:                   pf.examID,
				Topic:                    pf.topic,
				Part:                     pf.part,
				Mode:                     pf.mode,
				OrderStrategy:            pf.order,
				QuestionLimit:            pf.limit,
				QuestionNumber:           pf.question,
				TimeLimitSeconds:         timeLimitSec,
				QuestionTimeLimitSeconds: qTimeLimitSec,
				PlannedQuestions:         globalIDs(ordered),
			})
			if err != nil {
				fmt.Printf("error starting session: %v\n", err)
				continue
			}
			sessionID = id
			sessionStarted = true
		}

		elapsed := time.Since(start)
		result, err := c.SubmitAnswer(ctx, sessionID, q.GlobalID(), answer, lateFired, int(elapsed.Milliseconds()))
		if err != nil {
			fmt.Printf("error submitting answer: %v\n", err)
			continue
		}

		answered++
		switch {
		case result.Correct:
			correct++
			color.New(color.FgGreen, color.Bold).Println("✓ Correct!")
		case dontKnow:
			color.New(color.FgYellow, color.Bold).Printf("○ Marked as unknown. Correct answer: %s\n", result.CorrectAnswer)
		default:
			color.New(color.FgRed, color.Bold).Printf("✗ Incorrect. Correct answer: %s\n", result.CorrectAnswer)
		}
		if lateFired {
			timedOut++
		} else {
			onTime++
		}

		if result.Explanation != nil {
			fmt.Printf("\nTopic: %s\n", result.Explanation.Topic)
			rendered, err := glamour.Render(result.Explanation.Explanation, "dark")
			if err != nil {
				fmt.Println(result.Explanation.Explanation)
			} else {
				fmt.Println(rendered)
			}
			fmt.Print("Press Enter to continue...")
			stdin.ReadString('\n')
		}
	}

	if sessionStarted {
		if err := c.EndSession(ctx, sessionID, exitReason); err != nil {
			return err
		}
	}

	fmt.Println()
	examLabel := pf.examID
	if examLabel == "" {
		examLabel = pf.examType
	}
	fmt.Printf("Session: %s · %s mode\n", examLabel, pf.mode)
	fmt.Printf("Answered: %d / %d planned\n", answered, len(ordered))
	if answered > 0 {
		fmt.Printf("Correct:  %d (%.0f%%)\n", correct, float64(correct)/float64(answered)*100)
	} else {
		fmt.Printf("Correct:  0 (0%%)\n")
	}
	fmt.Printf("On time:  %d · Timed out: %d\n", onTime, timedOut)
	fmt.Printf("Exit: %s\n", exitReason)

	return nil
}

// runAnswerReveal walks ordered, showing each question's image immediately
// followed by its correct answer and explanation — a read-only reference
// mode. The user just presses Enter to advance ('q' to quit early); nothing
// is graded and no session/attempt rows are written to the progress DB.
func runAnswerReveal(c *core.Core, pf practiceFlags, ordered []*core.Question) error {
	stdin := bufio.NewReader(os.Stdin)
	var lastExternalImage string
	defer killExternalViewer(&lastExternalImage)

	for i, q := range ordered {
		fmt.Printf("\nQuestion %d of %d  (%s, q%d)\n", i+1, len(ordered), q.ExamID, q.ID)
		killExternalViewer(&lastExternalImage)
		if err := renderImage(c, q, pf.imageViewer, pf.dark, &lastExternalImage); err != nil {
			fmt.Printf("[image unavailable: %v]\n", err)
		}

		fmt.Printf("Correct answer: %s\n", answerLabel(q))
		if q.Explanation != nil {
			fmt.Printf("\nTopic: %s\n", q.Explanation.Topic)
			rendered, err := glamour.Render(q.Explanation.Explanation, "dark")
			if err != nil {
				fmt.Println(q.Explanation.Explanation)
			} else {
				fmt.Println(rendered)
			}
		}

		fmt.Print("\nPress Enter to continue ('q' to quit): ")
		line, _ := stdin.ReadString('\n')
		if strings.EqualFold(strings.TrimSpace(line), "q") {
			break
		}
	}
	return nil
}

// answerLabel returns the correct answer letter for display purposes,
// mirroring the precedence gradeAnswer uses for grading (core.gradeAnswer).
func answerLabel(q *core.Question) string {
	if q.SimpleAnswer != "" {
		return q.SimpleAnswer
	}
	if len(q.SubAnswers) > 0 {
		return q.SubAnswers[0].Answer
	}
	return "(unknown)"
}

func reviewFiltered(ctx context.Context, c *core.Core, pool []*core.Question) ([]*core.Question, error) {
	ids := make([]string, len(pool))
	for i, q := range pool {
		ids[i] = q.GlobalID()
	}
	failCounts, err := c.FailCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	var filtered []*core.Question
	for _, q := range pool {
		if failCounts[q.GlobalID()] > 0 {
			filtered = append(filtered, q)
		}
	}
	return filtered, nil
}

func filterByTopic(pool []*core.Question, topic string) []*core.Question {
	var filtered []*core.Question
	for _, q := range pool {
		if q.Topic() == topic {
			filtered = append(filtered, q)
		}
	}
	return filtered
}

func globalIDs(qs []*core.Question) []string {
	ids := make([]string, len(qs))
	for i, q := range qs {
		ids[i] = q.GlobalID()
	}
	return ids
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func orderQuestions(ctx context.Context, c *core.Core, pool []*core.Question, order string) ([]*core.Question, error) {
	ordered := make([]*core.Question, len(pool))
	copy(ordered, pool)

	switch order {
	case "sequential":
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].ExamID != ordered[j].ExamID {
				return ordered[i].ExamID < ordered[j].ExamID
			}
			return ordered[i].ID < ordered[j].ID
		})
	case "random":
		shuffle(ordered)
	case "fail-count", "fail-rate":
		ids := make([]string, len(ordered))
		for i, q := range ordered {
			ids[i] = q.GlobalID()
		}
		failCounts, err := c.FailCounts(ctx, ids)
		if err != nil {
			return nil, err
		}
		sort.Slice(ordered, func(i, j int) bool {
			return failCounts[ordered[i].GlobalID()] > failCounts[ordered[j].GlobalID()]
		})
	case "weak":
		// Weight towards topics with lower accuracy, including ones never
		// attempted yet (default weight below any known accuracy) — unlike
		// fail-count/fail-rate this is topic-level, not tied to a specific
		// question having been seen before.
		topicStats, err := c.GetTopicStats(ctx, core.ScopeAll)
		if err != nil {
			return nil, err
		}
		accuracyByTopic := make(map[string]float64, len(topicStats))
		for _, s := range topicStats {
			if s.Answered > 0 {
				accuracyByTopic[s.Topic] = s.Accuracy
			}
		}
		const noDataWeight = 0.5
		weight := func(q *core.Question) float64 {
			if acc, ok := accuracyByTopic[q.Topic()]; ok {
				return acc
			}
			return noDataWeight
		}
		shuffle(ordered) // randomize within topics of equal weakness
		sort.SliceStable(ordered, func(i, j int) bool { return weight(ordered[i]) < weight(ordered[j]) })
	default:
		return nil, fmt.Errorf("unknown order strategy %q", order)
	}
	return ordered, nil
}

func shuffle(qs []*core.Question) {
	for i := len(qs) - 1; i > 0; i-- {
		j := pseudoRand(i + 1)
		qs[i], qs[j] = qs[j], qs[i]
	}
}

var randState = uint64(time.Now().UnixNano())

func pseudoRand(n int) int {
	randState ^= randState << 13
	randState ^= randState >> 7
	randState ^= randState << 17
	if n <= 0 {
		return 0
	}
	return int(randState % uint64(n))
}

func renderImage(c *core.Core, q *core.Question, viewer string, dark bool, lastExternalImage *string) error {
	if viewer == "xdg-open" {
		return openImageExternally(c, q, dark, lastExternalImage)
	}

	if !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Println("[image rendering skipped: not a terminal]")
		return nil
	}
	img, err := c.Bank.Image(q)
	if err != nil {
		return err
	}

	if maxW, maxH, ok := terminalPixelBudget(); ok {
		img = fitImage(img, maxW, maxH)
	}
	if dark {
		img = core.InvertImage(img)
	}

	enc := sixel.NewEncoder(os.Stdout)
	if err := enc.Encode(img); err != nil {
		return err
	}
	// Some terminals don't reliably move the cursor below the sixel image, so
	// force a couple of blank rows to keep the answer prompt from overlapping it.
	fmt.Println()
	fmt.Println()
	return nil
}

// openImageExternally hands the question's image to the user's xdg-open
// handler, for terminals without sixel support. When dark is false, the
// embedded PNG is copied to a temp file as-is; when true, it's decoded,
// inverted, and re-encoded, since there's no external-viewer equivalent of
// sixel's in-memory image.Image path. lastExternalImage is updated so the
// caller can later kill whatever process picked it up (see killExternalViewer).
func openImageExternally(c *core.Core, q *core.Question, dark bool, lastExternalImage *string) error {
	tmp, err := os.CreateTemp("", "itpec-sensei-*.png")
	if err != nil {
		return err
	}
	defer tmp.Close()

	if dark {
		img, err := c.Bank.Image(q)
		if err != nil {
			return err
		}
		if err := png.Encode(tmp, core.InvertImage(img)); err != nil {
			return err
		}
	} else {
		imagesFS, err := c.Bank.ImagesFS()
		if err != nil {
			return err
		}
		src, err := imagesFS.Open(q.ImageRelPath())
		if err != nil {
			return err
		}
		defer src.Close()
		if _, err := io.Copy(tmp, src); err != nil {
			return err
		}
	}

	if err := exec.Command("xdg-open", tmp.Name()).Start(); err != nil {
		return fmt.Errorf("xdg-open: %w", err)
	}
	fmt.Printf("[image opened externally: %s]\n", tmp.Name())
	*lastExternalImage = tmp.Name()
	return nil
}

// killExternalViewer best-effort kills whatever process opened the previous
// externally-viewed image (xdg-open itself has already exited, so we match on
// the temp file path in the target process's argv instead — this only catches
// viewers that take the path as a literal argument, e.g. feh/eog/sxiv, not
// browser- or portal-based handlers). No-op if nothing was opened.
func killExternalViewer(lastExternalImage *string) {
	if *lastExternalImage == "" {
		return
	}
	_ = exec.Command("pkill", "-f", *lastExternalImage).Run()
	_ = os.Remove(*lastExternalImage)
	*lastExternalImage = ""
}

// terminalPixelBudget returns the usable pixel area of the controlling terminal,
// leaving a couple of text rows free for the prompt/feedback printed around the
// image. Returns ok=false if the terminal doesn't report pixel dimensions (e.g.
// some terminal emulators leave ws_xpixel/ws_ypixel at 0).
func terminalPixelBudget() (w, h int, ok bool) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 || ws.Row == 0 {
		return 0, 0, false
	}
	cellHeight := float64(ws.Ypixel) / float64(ws.Row)
	const reservedRows = 3 // room for the question header/prompt lines
	budgetH := float64(ws.Ypixel) - cellHeight*reservedRows
	if budgetH < cellHeight {
		budgetH = float64(ws.Ypixel)
	}
	return int(ws.Xpixel), int(budgetH), true
}

// fitImage scales img down (never up) to fit within maxW x maxH, preserving
// aspect ratio.
func fitImage(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 || (srcW <= maxW && srcH <= maxH) {
		return img
	}

	scale := min(float64(maxW)/float64(srcW), float64(maxH)/float64(srcH))
	dstW := max(1, int(float64(srcW)*scale))
	dstH := max(1, int(float64(srcH)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}
