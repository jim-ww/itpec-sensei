package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/jim-ww/itpec-sensei/core"
)

// NewRootCmd builds the itpec-sensei CLI command tree. Bare invocation (no
// subcommand) runs the progress summary, same as `itpec-sensei summary`
// would if that existed as its own subcommand.
func NewRootCmd() *cobra.Command {
	app := &App{}

	root := &cobra.Command{
		Use:           "itpec-sensei",
		Short:         "local-first ITPEC exam practice",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// cobra's auto-generated "completion" subcommand (used by
			// installShellCompletion at build time) shouldn't require
			// question data to be installed.
			if strings.HasPrefix(cmd.CommandPath(), "itpec-sensei completion") {
				return nil
			}
			return app.setup(cmd.Context())
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			return app.close()
		},
	}

	var topic, examID, part string
	var tags []string
	addScopeFlags(root.Flags(), &topic, &examID, &part, &tags, true)
	period := root.Flags().String("period", "all", "week | month | all")
	weakestTags := root.Flags().Int("weakest-tags", 10, "how many of your weakest tags to show (by lowest accuracy, min. 3 attempts each); 0 hides this section")
	root.Example = `  itpec-sensei
  itpec-sensei --exam=2025A_FE-A --period=week
  itpec-sensei --topic="Networks" --tags=cache-memory --weakest-tags=20`
	root.RunE = func(cmd *cobra.Command, args []string) error {
		scope, err := scopeFromFlags(topic, examID, part, tags)
		if err != nil {
			return err
		}
		return runSummary(cmd.Context(), app.Core, scope, core.Period(*period), *weakestTags)
	}

	root.AddCommand(
		newPracticeCmd(app),
		newHistoryCmd(app),
		newSessionsCmd(app),
		newExamsCmd(app),
		newExamCmd(app),
		newTopicsCmd(app),
		newTagsCmd(app),
		newResetCmd(app),
		newDataCmd(app),
		newServeCmd(app),
	)
	return root
}
