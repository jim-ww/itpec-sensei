// Command itpec-trainer is a local-first CLI + MCP server for ITPEC exam
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
	fmt.Println(`itpec-trainer — local-first ITPEC exam practice

Usage:
  itpec-trainer [--scope=all|topic:<name>|exam:<id>] [--period=week|month|all]
  itpec-trainer practice [--exam-type=fe|itpassport] [--exam=<id>] [--part=am|pm|all]
                          [--mode=normal|review] [--order=sequential|random|fail-count|fail-rate]
                          [--time-limit=<duration>] [--question-time-limit=<duration>]
  itpec-trainer reset <all|topic:<name>|exam:<id>> [--yes]
  itpec-trainer serve [--remote]`)
}
