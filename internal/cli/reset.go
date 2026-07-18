package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// newResetCmd implements `itpec-sensei reset <scope>`.
func newResetCmd(app *App) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:     "reset <all|topic:<name>|exam:<id>|part:am|part:pm>",
		Short:   "Clear progress for a scope",
		Args:    cobra.ExactArgs(1),
		Example: "  itpec-sensei reset exam:2025A_FE-A --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := core.Scope(args[0])

			if !yes && !confirm(fmt.Sprintf("This will permanently delete progress for scope %q. Continue? [y/N] ", scope)) {
				fmt.Println("Aborted.")
				return nil
			}

			if err := app.Core.ResetProgress(cmd.Context(), scope); err != nil {
				return fmt.Errorf("reset progress: %w", err)
			}
			fmt.Printf("Progress reset for scope %q.\n", scope)
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}
