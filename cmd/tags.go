package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newTagsCmd implements `itpec-sensei tags`, listing all known fine-grained
// question tags (see pdfparse/tags.json) — unlike topics, tags aren't
// AM/PM-scoped, so there's just one flat sorted list.
func newTagsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tags",
		Short: "List known question tags",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tags := app.Core.Bank.Tags()
			for _, tag := range tags {
				fmt.Println("  ", tag)
			}
			return nil
		},
	}
}
