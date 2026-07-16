// Command itpec-sensei is a local-first CLI + MCP server for ITPEC exam
// practice. See spec.md for the full architecture.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jim-ww/itpec-sensei/internal/cli"
	"github.com/jim-ww/itpec-sensei/internal/core"
	"github.com/jim-ww/itpec-sensei/internal/mcpserver"
)

func main() {
	ctx := context.Background()

	bank, err := core.LoadBank()
	if err != nil {
		log.Fatalf("load question bank: %v", err)
	}

	dbPath, err := core.DefaultDBPath()
	if err != nil {
		log.Fatalf("resolve progress db path: %v", err)
	}
	store, err := core.OpenStore(ctx, dbPath)
	if err != nil {
		log.Fatalf("open progress store: %v", err)
	}
	defer store.Close()

	c := core.New(bank, store)

	args := os.Args[1:]
	if len(args) == 0 {
		if err := cli.RunSummary(ctx, c, nil); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "practice":
		err = cli.RunPractice(ctx, c, rest)
	case "reset":
		err = cli.RunReset(ctx, c, rest)
	case "history":
		err = cli.RunHistory(ctx, c, rest)
	case "serve":
		err = mcpserver.Run(ctx, c, rest)
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		// Unknown first arg: treat as flags for the summary command.
		err = cli.RunSummary(ctx, c, args)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`itpec-sensei — local-first ITPEC exam practice

Usage:
  itpec-sensei [<command>] [flags]

Commands:
  (none)     Show progress summary (same as "summary")
  practice   Answer practice questions
  history    List past attempts (newest first)
  reset      Clear progress for a scope
  serve      Run the MCP server (stdio or --remote)

Flags for summary (default command):
  --scope <all|topic:NAME|exam:ID|part:am|part:pm>   default "all"
  --period <week|month|all>                          default "all"

Flags for practice:
  --exam-type <fe|itpassport>                                     default "fe"
  --exam <id>                                                     e.g. 2025A_FE-A
  --part <am|pm|all>                                              ignored if --exam is set
  --mode <normal|review>                                          default "normal"
  --order <sequential|random|fail-count|fail-rate>                default "random"
  --time-limit <duration>                                         whole-session limit, e.g. 150m
  --question-time-limit <duration>                                per-question limit, e.g. 90s
  --image-viewer <sixel|xdg-open>                                  default "sixel"

Flags for history:
  --scope <all|topic:NAME|exam:ID|part:am|part:pm>   default "all"
  --order <newest|oldest>                             default "newest"
  --limit <n>                                        default 20, 0 = all

Flags for reset:
  <all|topic:NAME|exam:ID|part:am|part:pm>   positional scope arg
  --yes                                       skip the confirmation prompt

Flags for serve:
  --remote   expose over Streamable HTTP instead of stdio
  --addr     listen address for --remote      default "127.0.0.1:8790"
  --ngrok    also forward a public ngrok tunnel

Examples:
  itpec-sensei
  itpec-sensei --scope=exam:2025A_FE-A --period=week
  itpec-sensei practice --exam=2025A_FE-A
  itpec-sensei practice --exam-type=fe --part=pm --mode=review
  itpec-sensei practice --exam=2025A_FE-A --time-limit=150m --question-time-limit=90s
  itpec-sensei reset exam:2025A_FE-A --yes
  itpec-sensei serve --remote --ngrok`)
}
