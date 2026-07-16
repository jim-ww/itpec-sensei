package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	sixel "github.com/mattn/go-sixel"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

type practiceFlags struct {
	examType            string
	examID              string
	mode                string
	order               string
	timeLimit           time.Duration
	questionTimeLimit   time.Duration
}

// RunPractice implements `itpec-trainer practice ...`.
func RunPractice(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("practice", flag.ExitOnError)
	examType := fs.String("exam-type", "fe", "fe | itpassport")
	examID := fs.String("exam", "", "scope to one exam id")
	mode := fs.String("mode", "normal", "normal | review")
	order := fs.String("order", "random", "sequential | random | fail-count | fail-rate")
	timeLimit := fs.Duration("time-limit", 0, "whole-session time limit, e.g. 150m")
	questionTimeLimit := fs.Duration("question-time-limit", 0, "per-question time limit, e.g. 90s")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pf := practiceFlags{
		examType:          *examType,
		examID:            *examID,
		mode:              *mode,
		order:             *order,
		timeLimit:         *timeLimit,
		questionTimeLimit: *questionTimeLimit,
	}
	return runPracticeSession(ctx, c, pf)
}

func runPracticeSession(ctx context.Context, c *core.Core, pf practiceFlags) error {
	planned := c.Bank.Questions("", pf.examID)
	if pf.mode == "review" {
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

	var timeLimitSec, qTimeLimitSec *int
	if pf.timeLimit > 0 {
		v := int(pf.timeLimit.Seconds())
		timeLimitSec = &v
	}
	if pf.questionTimeLimit > 0 {
		v := int(pf.questionTimeLimit.Seconds())
		qTimeLimitSec = &v
	}

	sessionID, err := c.StartSession(ctx, pf.examType, pf.examID, pf.mode, pf.order, timeLimitSec, qTimeLimitSec)
	if err != nil {
		return err
	}

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

questionLoop:
	for i, q := range ordered {
		fmt.Printf("\nQuestion %d of %d  (%s, q%d)\n", i+1, len(ordered), q.ExamID, q.ID)
		if err := renderImage(c, q); err != nil {
			fmt.Printf("[image unavailable: %v]\n", err)
		}

		answerCh := make(chan string, 1)
		go func() {
			fmt.Print("Your answer ('q' to quit, '?' if you don't know): ")
			line, _ := stdin.ReadString('\n')
			answerCh <- strings.TrimSpace(line)
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
			exitReason = "user_quit"
			break questionLoop
		}

		dontKnow := answer == "?" || strings.EqualFold(answer, "idk")
		if dontKnow {
			// Recorded as its own sentinel answer so it's distinguishable from a
			// wrong guess in the attempts log, but still grades as incorrect and
			// still counts as an answered question (the user did respond).
			answer = "idk"
		}

		elapsed := time.Since(start)
		result, err := c.SubmitAnswer(ctx, sessionID, q.ID, answer, lateFired, int(elapsed.Milliseconds()))
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
			color.New(color.FgYellow, color.Bold).Println("○ Marked as unknown — recorded as incorrect.")
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

	if err := c.EndSession(ctx, sessionID, exitReason); err != nil {
		return err
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

func reviewFiltered(ctx context.Context, c *core.Core, pool []*core.Question) ([]*core.Question, error) {
	ids := make([]int, len(pool))
	for i, q := range pool {
		ids[i] = q.ID
	}
	failCounts, err := c.FailCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	var filtered []*core.Question
	for _, q := range pool {
		if failCounts[q.ID] > 0 {
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
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	case "random":
		shuffle(ordered)
	case "fail-count", "fail-rate":
		ids := make([]int, len(ordered))
		for i, q := range ordered {
			ids[i] = q.ID
		}
		failCounts, err := c.FailCounts(ctx, ids)
		if err != nil {
			return nil, err
		}
		sort.Slice(ordered, func(i, j int) bool {
			return failCounts[ordered[i].ID] > failCounts[ordered[j].ID]
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

func renderImage(c *core.Core, q *core.Question) error {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Println("[image rendering skipped: not a terminal]")
		return nil
	}
	img, err := c.Bank.Image(q)
	if err != nil {
		return err
	}
	enc := sixel.NewEncoder(os.Stdout)
	return enc.Encode(img)
}
