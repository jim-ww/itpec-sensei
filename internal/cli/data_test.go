package cli

import (
	"os"
	"testing"
)

func TestPromptAndDownloadDecline(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	dataDir := t.TempDir()
	// exitOnDecline=false: declining must be a no-op, never call os.Exit.
	if err := promptAndDownload(t.Context(), dataDir, "v0.2.0", false, false); err != nil {
		t.Fatalf("promptAndDownload: %v", err)
	}
}
