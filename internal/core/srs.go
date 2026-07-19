package core

import (
	"context"
	"time"

	"github.com/jim-ww/itpec-sensei/internal/repository"
)

// srsMaxBox is the highest Leitner box a question can reach; box 1 is a
// question that's new or was just missed, srsMaxBox is one that's been
// answered correctly srsMaxBox-1 times in a row.
//
// Intervals are tuned for exam cram (weeks-to-months horizon, finite
// ~3000-question bank), not indefinite retention: box 1 is minutes, not a
// full day, so a fresh mistake can be immediately redrilled instead of
// waiting until tomorrow; the ceiling is 21 days, not months, since the
// exam itself is likely to happen well before a longer interval would ever
// come due.
const srsMaxBox = 6

// srsBoxInterval is how long a question sits in each box before it's due
// again. A wrong answer always resets to box 1, regardless of prior box.
var srsBoxInterval = map[int]time.Duration{
	1: 10 * time.Minute,
	2: 24 * time.Hour,
	3: 3 * 24 * time.Hour,
	4: 7 * 24 * time.Hour,
	5: 14 * 24 * time.Hour,
	6: 21 * 24 * time.Hour,
}

// nextSRSState computes questionID's new Leitner box and due date given its
// current box (1 if it has no prior state, i.e. this is its first attempt)
// and whether it was just answered correctly.
func nextSRSState(currentBox int, correct bool, now time.Time) repository.QuestionSRS {
	box := 1
	if correct {
		box = currentBox + 1
		if box > srsMaxBox {
			box = srsMaxBox
		}
	}
	return repository.QuestionSRS{
		Box:            box,
		DueAt:          now.Add(srsBoxInterval[box]),
		LastReviewedAt: now,
	}
}

// updateSRS records the outcome of answering questionID, advancing or
// resetting its Leitner box.
func (c *Core) updateSRS(ctx context.Context, questionID string, correct bool, now time.Time) error {
	current, found, err := c.Repo.GetQuestionSRS(ctx, questionID)
	if err != nil {
		return err
	}
	box := 1
	if found {
		box = current.Box
	}
	return c.Repo.UpsertQuestionSRS(ctx, questionID, nextSRSState(box, correct, now))
}
