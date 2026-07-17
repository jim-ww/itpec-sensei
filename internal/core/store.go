package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDBPath resolves the XDG-appropriate progress database path.
func DefaultDBPath() (string, error) {
	dataHome, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataHome, "itpec-sensei", "progress.db"), nil
}

// DefaultDataDir resolves the XDG-appropriate directory for the downloaded
// question bank data (see internal/core/datadownload.go).
func DefaultDataDir() (string, error) {
	dataHome, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataHome, "itpec-sensei", "data"), nil
}

func xdgDataHome() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome != "" {
		return dataHome, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}
