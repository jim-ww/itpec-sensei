package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// executeSession runs the answer loop over ordered. If existingSessionID is 0,
// a new sessions row is created lazily on the first submitted answer, as
// before; otherwise the caller is resuming an already-started session
// (--continue), so that id is used directly and no new row is created.
// ordered itself is never persisted — see planQuestions/planPool.
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

		if pf.explanations && result.Explanation != nil {
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
