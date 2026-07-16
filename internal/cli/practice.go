package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
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

type practiceFlags struct {
	examType          string
	examID            string
	part              string
	question          int
	limit             int
	mode              string
	order             string
	timeLimit         time.Duration
	questionTimeLimit time.Duration
	imageViewer       string
	showAnswer        bool
}

// RunPractice implements `itpec-sensei practice ...`.
func RunPractice(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("practice", flag.ExitOnError)
	examType := fs.String("exam-type", "fe", "fe | itpassport")
	examID := fs.String("exam", "", "scope to one exam id")
	part := fs.String("part", "all", "am | pm | all — which exam session to practice (e.g. FE-AM/FE-A vs FE-PM/FE-B); ignored if --exam is set")
	question := fs.Int("q", 0, "practice only this specific question number within --exam")
	limit := fs.Int("limit", 0, "max number of questions this session (0 = no limit)")
	mode := fs.String("mode", "normal", "normal | review")
	order := fs.String("order", "random", "sequential | random | fail-count | fail-rate")
	timeLimit := fs.Duration("time-limit", 0, "whole-session time limit, e.g. 150m")
	questionTimeLimit := fs.Duration("question-time-limit", 0, "per-question time limit, e.g. 90s")
	imageViewer := fs.String("image-viewer", "sixel", "sixel | xdg-open — how to display question images")
	showAnswer := fs.Bool("answer", false, "reveal the correct answer/explanation immediately per question instead of grading input; no DB writes in this mode")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch *imageViewer {
	case "sixel", "xdg-open":
	default:
		return fmt.Errorf("invalid --image-viewer %q, expected sixel or xdg-open", *imageViewer)
	}

	if *question > 0 && *examID == "" {
		return fmt.Errorf("-q requires --exam")
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
		question:          *question,
		limit:             *limit,
		mode:              *mode,
		order:             *order,
		timeLimit:         *timeLimit,
		questionTimeLimit: *questionTimeLimit,
		imageViewer:       *imageViewer,
		showAnswer:        *showAnswer,
	}
	return runPracticeSession(ctx, c, pf)
}

func runPracticeSession(ctx context.Context, c *core.Core, pf practiceFlags) error {
	var planned []*core.Question
	switch {
	case pf.question > 0:
		q := c.Bank.QuestionByExamAndNumber(pf.examID, pf.question)
		if q == nil {
			return fmt.Errorf("question %s#%d not found", pf.examID, pf.question)
		}
		planned = []*core.Question{q}
	case pf.examID != "":
		planned = c.Bank.Questions("", pf.examID)
	default:
		planned = c.Bank.QuestionsForExams(c.Bank.ExamsByPart(pf.part))
	}
	if pf.mode == "review" && pf.question == 0 {
		var err error
		planned, err = reviewFiltered(ctx, c, planned)
		if err != nil {
			return err
		}
	}
	if len(planned) == 0 {
		fmt.Println("No questions match this filter.")
		return nil
	}

	ordered, err := orderQuestions(ctx, c, planned, pf.order)
	if err != nil {
		return err
	}

	if pf.limit > 0 && pf.limit < len(ordered) {
		ordered = ordered[:pf.limit]
	}

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

	var sessionID int64
	var sessionStarted bool

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
		if err := renderImage(c, q, pf.imageViewer, &lastExternalImage); err != nil {
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
			id, err := c.StartSession(ctx, pf.examType, pf.examID, pf.mode, pf.order, timeLimitSec, qTimeLimitSec)
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
			color.New(color.FgYellow, color.Bold).Println("○ Marked as unknown")
		default:
			color.New(color.FgRed, color.Bold).Println("✗ Incorrect.")
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
		if err := renderImage(c, q, pf.imageViewer, &lastExternalImage); err != nil {
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

func renderImage(c *core.Core, q *core.Question, viewer string, lastExternalImage *string) error {
	if viewer == "xdg-open" {
		return openImageExternally(c, q, lastExternalImage)
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

// openImageExternally copies the question's embedded image to a temp file and
// hands it to the user's xdg-open handler, for terminals without sixel support.
// lastExternalImage is updated so the caller can later kill whatever process
// picked it up (see killExternalViewer).
func openImageExternally(c *core.Core, q *core.Question, lastExternalImage *string) error {
	imagesFS, err := c.Bank.ImagesFS()
	if err != nil {
		return err
	}
	src, err := imagesFS.Open(q.ImageRelPath())
	if err != nil {
		return err
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "itpec-sensei-*.png")
	if err != nil {
		return err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, src); err != nil {
		return err
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
