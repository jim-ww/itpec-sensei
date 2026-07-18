package cli

import (
	"github.com/spf13/cobra"

	"github.com/jim-ww/itpec-sensei/internal/mcpserver"
)

// newServeCmd implements `itpec-sensei serve [--remote]`.
func newServeCmd(app *App) *cobra.Command {
	var remote, useNgrok bool
	var addr, imageViewer string

	cmd := &cobra.Command{
		Use:     "serve",
		Short:   "Run the MCP server (stdio or --remote)",
		Args:    cobra.NoArgs,
		Example: "  itpec-sensei serve --remote --ngrok",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcpserver.Run(cmd.Context(), app.Core, mcpserver.Options{
				Remote:      remote,
				Addr:        addr,
				UseNgrok:    useNgrok,
				ImageViewer: imageViewer,
			})
		},
	}

	cmd.Flags().BoolVar(&remote, "remote", false, "expose over Streamable HTTP instead of stdio")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8790", "listen address for --remote")
	cmd.Flags().BoolVar(&useNgrok, "ngrok", false, "also forward a public ngrok tunnel")
	cmd.Flags().StringVar(&imageViewer, "image-viewer", "xdg-open", "local command the MCP open_question_image tool uses to open images on the machine running the server (bypasses the MCP client's own image rendering)")
	return cmd
}
