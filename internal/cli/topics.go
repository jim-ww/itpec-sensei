package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunTopics implements `itpec-sensei topics`, listing all known topics.
func RunTopics(ctx context.Context, c *core.Core, args []string) error {
	fs := flag.NewFlagSet("topics", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	am, pm, other, err := c.ListTopicsByPart(ctx)
	if err != nil {
		return fmt.Errorf("list topics: %w", err)
	}

	fmt.Println("itpec-sensei — topics")
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
}
