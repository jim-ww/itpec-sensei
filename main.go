// Command itpec-sensei is a local-first CLI + MCP server for ITPEC exam
// practice. See spec.md for the full architecture.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jim-ww/itpec-sensei/cmd"
)

func main() {
	if err := cmd.NewRootCmd().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
