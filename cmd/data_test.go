package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptAndDownloadDecline(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	dataDir := t.TempDir()
	// exitOnDecline=false: declining must be a no-op, never call os.Exit.
	require.NoError(t, promptAndDownload(t.Context(), dataDir, "v0.2.0", false, false))
}
