package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jim-ww/itpec-sensei/internal/core"
	"github.com/jim-ww/itpec-sensei/internal/repository/sqlite"
)

// App holds the state shared across all subcommands: the question bank +
// progress store (Core), built once by the root command's
// PersistentPreRunE, and the resolved data directory (needed standalone by
// the "data" subcommand, which must work even before data is installed).
type App struct {
	DataDir string
	Core    *core.Core

	closeDB func() error
}

// resolveDataDir resolves DataDir without touching the question bank or
// progress DB — used by the "data" subcommand, which manages the data
// directory itself and must run before data is necessarily present.
func (a *App) resolveDataDir() error {
	dir, err := core.DefaultDataDir()
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	a.DataDir = dir
	return nil
}

// setup resolves the data dir, ensures question data is installed (prompting
// if needed), and opens the question bank + progress store into Core. Used
// by every subcommand except "data".
func (a *App) setup(ctx context.Context) error {
	if err := a.resolveDataDir(); err != nil {
		return err
	}
	if err := EnsureData(ctx, a.DataDir); err != nil {
		return err
	}

	bank, err := core.LoadBank(filepath.Join(a.DataDir, "questions"))
	if err != nil {
		return fmt.Errorf("load question bank: %w", err)
	}

	dbPath, err := core.DefaultDBPath()
	if err != nil {
		return fmt.Errorf("resolve progress db path: %w", err)
	}
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open progress store: %w", err)
	}
	a.closeDB = db.Close
	a.Core = core.New(bank, sqlite.NewRepository(db))
	return nil
}

// close releases resources acquired by setup, if it ran.
func (a *App) close() error {
	if a.closeDB == nil {
		return nil
	}
	return a.closeDB()
}
