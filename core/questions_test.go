package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWeightedPickByTopicWeakness(t *testing.T) {
	bank := newTestBank(t)
	q1 := bank.QuestionByExamAndNumber("2020A_FE-A", 1)

	t.Run("single-question pool always returns that question", func(t *testing.T) {
		got := weightedPickByTopicWeakness([]*Question{q1}, map[string]float64{"Networks": 0.9})
		assert.Equal(t, q1.GlobalID(), got.GlobalID())
	})

	t.Run("topic with no accuracy data still gets picked over many trials", func(t *testing.T) {
		q2 := bank.QuestionByExamAndNumber("2020A_FE-A", 2) // "Security", no accuracy data
		pool := []*Question{q1, q2}
		accuracy := map[string]float64{"Networks": 1.0} // Networks perfect, Security unknown
		seenSecurity := false
		for range 200 {
			got := weightedPickByTopicWeakness(pool, accuracy)
			if got.Topic() == "Security" {
				seenSecurity = true
				break
			}
		}
		assert.True(t, seenSecurity, "expected Security (no data) to surface at least once across 200 draws")
	})
}
