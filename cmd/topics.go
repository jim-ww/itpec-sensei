package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newTopicsCmd implements `itpec-sensei topics`, listing all known topics.
func newTopicsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "topics",
		Short: "List known topics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			am, pm, other, err := app.Core.ListTopicsByPart(ctx)
			if err != nil {
				return fmt.Errorf("list topics: %w", err)
			}

			printGroup := func(label string, topics []string) {
				if len(topics) == 0 {
					return
				}
				fmt.Println(label + ":")
				for _, topic := range topics {
					fmt.Println("  ", topic)
				}
			}
			printGroup("AM", am)
			printGroup("PM", pm)
			printGroup("Other", other)
			return nil
		},
	}
}
