package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunReset implements `itpec-sensei reset <scope>`.
func RunReset(ctx context.Context, c *core.Core, args []string) error {
	// --yes may appear before or after the positional scope argument, so scan
	// for it manually rather than relying on flag.Parse's positional-args-stop
	// behavior.
	yesVal := false
	var rest []string
	for _, a := range args {
		if a == "--yes" || a == "-yes" {
			yesVal = true
			continue
		}
		rest = append(rest, a)
	}
	yes := &yesVal

	if len(rest) != 1 {
		return fmt.Errorf("usage: itpec-sensei reset <all|topic:<name>|exam:<id>|part:am|part:pm> [--yes]")
	}
	scope := core.Scope(rest[0])

	if !*yes {
		fmt.Printf("This will permanently delete progress for scope %q. Continue? [y/N] ", scope)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "y" && line != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := c.ResetProgress(ctx, scope); err != nil {
		return fmt.Errorf("reset progress: %w", err)
	}
	fmt.Printf("Progress reset for scope %q.\n", scope)
	return nil
}
