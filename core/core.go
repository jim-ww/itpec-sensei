package core

// Core bundles the question bank and progress store, exposing the shared
// business logic used by both the CLI and the MCP server. All persistence
// goes through Repo (see repository.go) — Core itself contains no SQL, so
// its logic (grading, scoping, stats aggregation, session resume/repeat) can
// be tested against a fake Repository.
//
// Core's methods are grouped across files by responsibility:
// questions.go (drawing/grading questions), sessions.go (session
// lifecycle), history.go (attempt/session listing), stats.go (progress
// aggregation), scope.go (Scope resolution), misc.go (topics/exams/reset).
type Core struct {
	Bank *Bank
	Repo Repository
}

// New builds a Core over bank and repo.
func New(bank *Bank, repo Repository) *Core {
	return &Core{Bank: bank, Repo: repo}
}
