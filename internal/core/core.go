package core

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// Core bundles the question bank and progress store, exposing the shared
// business logic used by both the CLI and the MCP server.
type Core struct {
	Bank  *Bank
	Store *sql.DB
}

func New(bank *Bank, store *sql.DB) *Core {
	return &Core{Bank: bank, Store: store}
}

// GetNextQuestion returns a question matching filter. It never includes the
// answer or explanation — those are only ever exposed via SubmitAnswer.
func (c *Core) GetNextQuestion(ctx context.Context, filter QuestionFilter) (*Question, error) {
	pool := c.Bank.Questions(filter.Topic, filter.ExamID)
	if len(pool) == 0 {
		return nil, fmt.Errorf("no questions match filter")
	}

	if strings.EqualFold(filter.Mode, "review") {
		reviewIDs, err := c.reviewQueueIDs(ctx)
		if err != nil {
			return nil, err
		}
		var filtered []*Question
		for _, q := range pool {
			if reviewIDs[q.GlobalID()] {
				filtered = append(filtered, q)
			}
		}
		pool = filtered
		if len(pool) == 0 {
			return nil, fmt.Errorf("no questions in review queue for this filter")
		}
	}

	pick := pool[rand.Intn(len(pool))]
	return stripAnswer(pick), nil
}

// GetQuestion looks up one question by exam ID + question number. When
// revealAnswer is false, the answer/explanation are stripped, same as
// GetNextQuestion. When true, the full question (including the correct
// answer and explanation) is returned — a deliberate, explicit escape hatch
// for reference lookups, not used by the normal practice/grading flow.
func (c *Core) GetQuestion(ctx context.Context, examID string, number int, revealAnswer bool) (*Question, error) {
	q := c.Bank.QuestionByExamAndNumber(examID, number)
	if q == nil {
		return nil, fmt.Errorf("question %s#%d not found", examID, number)
	}
	if revealAnswer {
		return q, nil
	}
	return stripAnswer(q), nil
}

func stripAnswer(q *Question) *Question {
	cp := *q
	cp.Answer = nil
	cp.SimpleAnswer = ""
	cp.SubAnswers = nil
	cp.Explanation = nil
	return &cp
}

// SubmitAnswer grades answer against the embedded correct answer for questionID
// (a Question.GlobalID(), as returned by GetNextQuestion), records the attempt,
// and returns correctness + explanation.
func (c *Core) SubmitAnswer(ctx context.Context, sessionID int64, questionID string, answer string, timedOut bool, timeTakenMs int) (*AnswerResult, error) {
	q := c.Bank.Question(questionID)
	if q == nil {
		return nil, fmt.Errorf("unknown question id %q", questionID)
	}

	correct := gradeAnswer(q, answer)

	_, err := c.Store.ExecContext(ctx,
		`INSERT INTO attempts (session_id, question_id, answer, correct, timed_out, time_taken_ms, answered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, questionID, answer, correct, timedOut, timeTakenMs, time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("record attempt: %w", err)
	}

	return &AnswerResult{Correct: correct, Explanation: q.Explanation}, nil
}

func gradeAnswer(q *Question, answer string) bool {
	answer = strings.TrimSpace(strings.ToLower(answer))
	if q.SimpleAnswer != "" {
		return strings.EqualFold(q.SimpleAnswer, answer)
	}
	if len(q.SubAnswers) > 0 {
		// Simple exams only submit a single letter; for multi-part questions,
		// treat the submission as matching the first sub-answer's expected letter.
		return strings.EqualFold(q.SubAnswers[0].Answer, answer)
	}
	return false
}

// StartSession creates a new sessions row and returns its ID.
func (c *Core) StartSession(ctx context.Context, examType, examID, mode, orderStrategy string, timeLimitSec, questionTimeLimitSec *int) (int64, error) {
	res, err := c.Store.ExecContext(ctx,
		`INSERT INTO sessions (started_at, exam_type, exam_id, mode, order_strategy, time_limit_seconds, question_time_limit_seconds)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC(), examType, nullableString(examID), mode, orderStrategy, timeLimitSec, questionTimeLimitSec,
	)
	if err != nil {
		return 0, fmt.Errorf("start session: %w", err)
	}
	return res.LastInsertId()
}

// EndSession marks a session finished with the given exit reason.
func (c *Core) EndSession(ctx context.Context, sessionID int64, exitReason string) error {
	_, err := c.Store.ExecContext(ctx,
		`UPDATE sessions SET ended_at = ?, exit_reason = ? WHERE id = ?`,
		time.Now().UTC(), exitReason, sessionID,
	)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetProgressSummary computes overall accuracy/streak/heatmap/review-queue for scope+period.
func (c *Core) GetProgressSummary(ctx context.Context, scope Scope, period Period) (*ProgressSummary, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}

	where, args := c.scopeWhere(ids)
	periodClause, periodArgs := periodWhere(period)
	if periodClause != "" {
		if where == "" {
			where = periodClause
		} else {
			where += " AND " + periodClause
		}
		args = append(args, periodArgs...)
	}

	query := `SELECT question_id, correct, answered_at, time_taken_ms FROM attempts`
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := c.Store.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attempts: %w", err)
	}
	defer rows.Close()

	summary := &ProgressSummary{Heatmap: make(map[string]int)}
	type partAcc struct {
		answered, correct int
		times             []int
	}
	byPart := make(map[string]*partAcc)
	for rows.Next() {
		var questionID string
		var correct bool
		var answeredAt time.Time
		var timeTakenMs int
		if err := rows.Scan(&questionID, &correct, &answeredAt, &timeTakenMs); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		summary.Answered++
		day := answeredAt.Format("2006-01-02")
		summary.Heatmap[day]++

		part := "other"
		if q := c.Bank.Question(questionID); q != nil {
			if p := ExamPart(q.ExamID); p != "" {
				part = p
			}
		}
		a := byPart[part]
		if a == nil {
			a = &partAcc{}
			byPart[part] = a
		}
		a.answered++
		if correct {
			a.correct++
		}
		if timeTakenMs > 0 {
			a.times = append(a.times, timeTakenMs)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}
	summary.Streak = computeStreak(summary.Heatmap)
	summary.MaxStreak = computeMaxStreak(summary.Heatmap)

	for _, part := range []string{"am", "pm", "other"} {
		a := byPart[part]
		if a == nil {
			continue
		}
		ps := PartStat{Part: part, Answered: a.answered, Correct: a.correct}
		if a.answered > 0 {
			ps.Accuracy = float64(a.correct) / float64(a.answered)
		}
		ps.AvgTimeMs, ps.MedianTimeMs = timeStats(a.times)
		summary.PartStats = append(summary.PartStats, ps)
	}

	reviewIDs, err := c.reviewQueueIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		count := 0
		for id := range reviewIDs {
			if _, ok := ids[id]; ok {
				count++
			}
		}
		summary.ReviewQueue = count
	} else {
		summary.ReviewQueue = len(reviewIDs)
	}

	return summary, nil
}

// timeStats returns the mean and median of ms, or (0, 0) if ms is empty.
func timeStats(ms []int) (avg, median float64) {
	if len(ms) == 0 {
		return 0, 0
	}
	sum := 0
	for _, v := range ms {
		sum += v
	}
	avg = float64(sum) / float64(len(ms))

	sorted := append([]int(nil), ms...)
	sort.Ints(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		median = float64(sorted[mid-1]+sorted[mid]) / 2
	} else {
		median = float64(sorted[mid])
	}
	return avg, median
}

func computeStreak(heatmap map[string]int) int {
	streak := 0
	day := time.Now().UTC()
	// If today has no activity yet, start counting from yesterday.
	if heatmap[day.Format("2006-01-02")] == 0 {
		day = day.AddDate(0, 0, -1)
	}
	for {
		key := day.Format("2006-01-02")
		if heatmap[key] == 0 {
			break
		}
		streak++
		day = day.AddDate(0, 0, -1)
	}
	return streak
}

// computeMaxStreak returns the longest run of consecutive days with any
// recorded activity, over the entire heatmap — unlike computeStreak, this
// isn't anchored to "today" and doesn't reset once the current streak breaks.
func computeMaxStreak(heatmap map[string]int) int {
	if len(heatmap) == 0 {
		return 0
	}
	days := make([]time.Time, 0, len(heatmap))
	for k, count := range heatmap {
		if count == 0 {
			continue
		}
		t, err := time.Parse("2006-01-02", k)
		if err != nil {
			continue
		}
		days = append(days, t)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	max, cur := 0, 0
	for i, d := range days {
		if i == 0 || d.Sub(days[i-1]) != 24*time.Hour {
			cur = 1
		} else {
			cur++
		}
		if cur > max {
			max = cur
		}
	}
	return max
}

func periodWhere(period Period) (string, []any) {
	switch period {
	case PeriodWeek:
		return "answered_at >= ?", []any{time.Now().UTC().AddDate(0, 0, -7)}
	case PeriodMonth:
		return "answered_at >= ?", []any{time.Now().UTC().AddDate(0, -1, 0)}
	default:
		return "", nil
	}
}

// GetTopicStats returns per-topic answered/correct/accuracy for scope.
func (c *Core) GetTopicStats(ctx context.Context, scope Scope) ([]TopicStat, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}
	where, args := c.scopeWhere(ids)
	query := `SELECT question_id, correct FROM attempts`
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := c.Store.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attempts: %w", err)
	}
	defer rows.Close()

	type acc struct{ answered, correct int }
	byTopic := make(map[string]*acc)
	for rows.Next() {
		var qid string
		var correct bool
		if err := rows.Scan(&qid, &correct); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		q := c.Bank.Question(qid)
		topic := "Uncategorized"
		if q != nil {
			topic = q.Topic()
		}
		a := byTopic[topic]
		if a == nil {
			a = &acc{}
			byTopic[topic] = a
		}
		a.answered++
		if correct {
			a.correct++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}

	var stats []TopicStat
	for topic, a := range byTopic {
		s := TopicStat{Topic: topic, Answered: a.answered, Correct: a.correct}
		if a.answered > 0 {
			s.Accuracy = float64(a.correct) / float64(a.answered)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// GetExamStats returns per-exam answered/correct/accuracy for scope, mirroring GetTopicStats.
func (c *Core) GetExamStats(ctx context.Context, scope Scope) ([]ExamStat, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}
	where, args := c.scopeWhere(ids)
	query := `SELECT question_id, correct FROM attempts`
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := c.Store.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attempts: %w", err)
	}
	defer rows.Close()

	type acc struct{ answered, correct int }
	byExam := make(map[string]*acc)
	for rows.Next() {
		var qid string
		var correct bool
		if err := rows.Scan(&qid, &correct); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		examID := "Unknown"
		if q := c.Bank.Question(qid); q != nil {
			examID = q.ExamID
		}
		a := byExam[examID]
		if a == nil {
			a = &acc{}
			byExam[examID] = a
		}
		a.answered++
		if correct {
			a.correct++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}

	var stats []ExamStat
	for examID, a := range byExam {
		s := ExamStat{ExamID: examID, Answered: a.answered, Correct: a.correct}
		if a.answered > 0 {
			s.Accuracy = float64(a.correct) / float64(a.answered)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// HistoryOrder selects chronological direction for GetHistory.
type HistoryOrder string

const (
	HistoryNewestFirst HistoryOrder = "newest"
	HistoryOldestFirst HistoryOrder = "oldest"
)

// GetHistory returns attempts matching scope, ordered by order (newest or
// oldest first), capped at limit (0 means no cap). Each record is joined with
// question metadata (topic, examId) at query time, since attempts only store
// the raw question id.
func (c *Core) GetHistory(ctx context.Context, scope Scope, order HistoryOrder, limit int) ([]AttemptRecord, error) {
	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return nil, err
	}
	where, args := c.scopeWhere(ids)

	query := `SELECT question_id, answer, correct, timed_out, time_taken_ms, answered_at FROM attempts`
	if where != "" {
		query += " WHERE " + where
	}
	if order == HistoryOldestFirst {
		query += " ORDER BY answered_at ASC"
	} else {
		query += " ORDER BY answered_at DESC"
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := c.Store.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attempts: %w", err)
	}
	defer rows.Close()

	var records []AttemptRecord
	for rows.Next() {
		var r AttemptRecord
		var timeTakenMs int
		if err := rows.Scan(&r.QuestionID, &r.Answer, &r.Correct, &r.TimedOut, &timeTakenMs, &r.AnsweredAt); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		r.TimeTakenMs = timeTakenMs
		if q := c.Bank.Question(r.QuestionID); q != nil {
			r.ExamID = q.ExamID
			r.Topic = q.Topic()
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}
	return records, nil
}

// GetSessions returns past practice sessions (newest/oldest first per order),
// each with its aggregate answered/correct count joined from attempts. scope
// supports "all", "exam:<id>", and "part:am|pm" (matched against a session's
// own exam_id) — unlike GetHistory/GetTopicStats, "topic:<name>" isn't
// supported here, since a session isn't inherently scoped to one topic.
func (c *Core) GetSessions(ctx context.Context, scope Scope, order HistoryOrder, limit int) ([]SessionRecord, error) {
	if err := validateSessionScope(scope); err != nil {
		return nil, err
	}

	query := `
		SELECT s.id, s.started_at, s.ended_at, s.exam_type, s.exam_id, s.mode, s.order_strategy,
		       s.time_limit_seconds, s.question_time_limit_seconds, s.exit_reason,
		       COUNT(a.id), COALESCE(SUM(CASE WHEN a.correct THEN 1 ELSE 0 END), 0)
		FROM sessions s
		LEFT JOIN attempts a ON a.session_id = s.id
		GROUP BY s.id`
	if order == HistoryOldestFirst {
		query += " ORDER BY s.started_at ASC"
	} else {
		query += " ORDER BY s.started_at DESC"
	}

	rows, err := c.Store.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var records []SessionRecord
	for rows.Next() {
		var r SessionRecord
		var examID, mode, orderStrategy, exitReason sql.NullString
		var endedAt sql.NullTime
		var timeLimitSec, qTimeLimitSec sql.NullInt64
		if err := rows.Scan(&r.ID, &r.StartedAt, &endedAt, &r.ExamType, &examID, &mode, &orderStrategy,
			&timeLimitSec, &qTimeLimitSec, &exitReason, &r.Answered, &r.Correct); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		r.ExamID = examID.String
		r.Mode = mode.String
		r.OrderStrategy = orderStrategy.String
		r.ExitReason = exitReason.String
		if endedAt.Valid {
			t := endedAt.Time
			r.EndedAt = &t
		}
		if timeLimitSec.Valid {
			v := int(timeLimitSec.Int64)
			r.TimeLimitSeconds = &v
		}
		if qTimeLimitSec.Valid {
			v := int(qTimeLimitSec.Int64)
			r.QuestionTimeLimitSeconds = &v
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	records = filterSessionsByScope(records, scope)
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}
	return records, nil
}

func validateSessionScope(scope Scope) error {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return nil
	}
	kind, _, ok := strings.Cut(s, ":")
	if !ok {
		return fmt.Errorf("invalid scope %q, expected all|exam:<id>|part:<am|pm>", scope)
	}
	switch kind {
	case "exam", "part":
		return nil
	case "topic":
		return fmt.Errorf("topic scope is not supported for sessions (a session isn't scoped to one topic)")
	default:
		return fmt.Errorf("invalid scope kind %q, expected exam or part", kind)
	}
}

func filterSessionsByScope(records []SessionRecord, scope Scope) []SessionRecord {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return records
	}
	kind, value, _ := strings.Cut(s, ":")
	var filtered []SessionRecord
	for _, r := range records {
		switch kind {
		case "exam":
			if r.ExamID == value {
				filtered = append(filtered, r)
			}
		case "part":
			if ExamPart(r.ExamID) == strings.ToLower(value) {
				filtered = append(filtered, r)
			}
		}
	}
	return filtered
}

// ListTopics returns all topics present in the question bank.
func (c *Core) ListTopics(ctx context.Context) ([]string, error) {
	return c.Bank.Topics(), nil
}

// ListExams returns all exam IDs present in the question bank.
func (c *Core) ListExams(ctx context.Context) ([]string, error) {
	return c.Bank.Exams(), nil
}

// ResetProgress deletes attempts (and their sessions where scope is "all") matching scope.
func (c *Core) ResetProgress(ctx context.Context, scope Scope) error {
	if scope == ScopeAll {
		if _, err := c.Store.ExecContext(ctx, `DELETE FROM attempts`); err != nil {
			return fmt.Errorf("reset attempts: %w", err)
		}
		if _, err := c.Store.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
			return fmt.Errorf("reset sessions: %w", err)
		}
		return nil
	}

	ids, err := c.scopeQuestionIDs(scope)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	query := fmt.Sprintf(`DELETE FROM attempts WHERE question_id IN (%s)`, strings.Join(placeholders, ","))
	if _, err := c.Store.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("reset scoped attempts: %w", err)
	}
	return nil
}

// scopeQuestionIDs resolves a Scope to the set of matching question global IDs.
// Returns nil for ScopeAll (meaning "no filter").
func (c *Core) scopeQuestionIDs(scope Scope) (map[string]struct{}, error) {
	s := string(scope)
	if s == "" || Scope(s) == ScopeAll {
		return nil, nil
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid scope %q, expected all|topic:<name>|exam:<id>|part:<am|pm>", scope)
	}
	kind, value := parts[0], parts[1]
	var pool []*Question
	switch kind {
	case "topic":
		pool = c.Bank.Questions(value, "")
	case "exam":
		pool = c.Bank.Questions("", value)
	case "part":
		value = strings.ToLower(value)
		if value != "am" && value != "pm" {
			return nil, fmt.Errorf("invalid part %q, expected am or pm", value)
		}
		pool = c.Bank.QuestionsForExams(c.Bank.ExamsByPart(value))
	default:
		return nil, fmt.Errorf("invalid scope kind %q, expected topic, exam, or part", kind)
	}
	ids := make(map[string]struct{}, len(pool))
	for _, q := range pool {
		ids[q.GlobalID()] = struct{}{}
	}
	return ids, nil
}

func (c *Core) scopeWhere(ids map[string]struct{}) (string, []any) {
	if ids == nil {
		return "", nil
	}
	if len(ids) == 0 {
		return "1=0", nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	return fmt.Sprintf("question_id IN (%s)", strings.Join(placeholders, ",")), args
}

// reviewQueueIDs returns the set of question global IDs whose most recent attempt was incorrect.
func (c *Core) reviewQueueIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := c.Store.QueryContext(ctx, `
		SELECT a.question_id
		FROM attempts a
		WHERE a.answered_at = (
			SELECT MAX(answered_at) FROM attempts WHERE question_id = a.question_id
		) AND a.correct = 0
	`)
	if err != nil {
		return nil, fmt.Errorf("query review queue: %w", err)
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan review queue: %w", err)
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review queue: %w", err)
	}
	return ids, nil
}

// FailCounts returns, for the given question global IDs, how many times each has
// been answered incorrectly (used by the fail-count order strategy).
func (c *Core) FailCounts(ctx context.Context, ids []string) (map[string]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := c.Store.QueryContext(ctx, fmt.Sprintf(
		`SELECT question_id, COUNT(*) FROM attempts WHERE correct = 0 AND question_id IN (%s) GROUP BY question_id`,
		strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("query fail counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan fail count: %w", err)
		}
		counts[id] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fail counts: %w", err)
	}
	return counts, nil
}
