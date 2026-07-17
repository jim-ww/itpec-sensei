// Command itpec-sensei is a local-first CLI + MCP server for ITPEC exam
// practice. See spec.md for the full architecture.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jim-ww/itpec-sensei/internal/cli"
	"github.com/jim-ww/itpec-sensei/internal/core"
	"github.com/jim-ww/itpec-sensei/internal/mcpserver"
)

func main() {
	ctx := context.Background()

	dataDir, err := core.DefaultDataDir()
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}

	args := os.Args[1:]
	sub, rest := "", args
	if len(args) > 0 {
		sub, rest = args[0], args[1:]
	}

	if sub == "-h" || sub == "--help" || sub == "help" {
		printUsage()
		return
	}

	// "data" manages dataDir itself, so it must work even before data is
	// installed — everything else needs the bank, which needs data present.
	if sub == "data" {
		if err := cli.RunData(ctx, dataDir, rest); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if err := cli.EnsureData(ctx, dataDir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	bank, err := core.LoadBank(filepath.Join(dataDir, "questions"))
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

	if len(args) == 0 {
		if err := cli.RunSummary(ctx, c, nil); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	switch sub {
	case "practice":
		err = cli.RunPractice(ctx, c, rest)
	case "reset":
		err = cli.RunReset(ctx, c, rest)
	case "history":
		err = cli.RunHistory(ctx, c, rest)
	case "sessions":
		err = cli.RunSessions(ctx, c, rest)
	case "exams":
		err = cli.RunExams(ctx, c, rest)
	case "exam":
		err = cli.RunExam(ctx, c, rest)
	case "topics":
		err = cli.RunTopics(ctx, c, rest)
	case "serve":
		err = mcpserver.Run(ctx, c, rest)
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
  sessions   List past practice sessions (newest first)
  exams      List known exam IDs
  exam       Show readable metadata + your progress for one exam
  topics     List known topics
  reset      Clear progress for a scope
  data       Check/install question data (auto-prompted on first run)
  serve      Run the MCP server (stdio or --remote)

Flags for summary (default command):
  --scope <all|topic:NAME|exam:ID|part:am|part:pm>   default "all"
  --period <week|month|all>                          default "all"

Flags for practice:
  --exam-type <fe|itpassport>                                     default "fe"
  --exam <id>                                                     e.g. 2025A_FE-A
  --part <am|pm|all>                                              ignored if --exam is set
  --topic <name>                                                  filter to one topic; combines with --exam/--part (see "itpec-sensei topics")
  --q <n>                                                         practice only this question number within --exam
  --limit <n>                                                     max questions this session, default 0 (no limit)
  --mode <normal|review>                                          default "normal"
  --order <sequential|random|fail-count|fail-rate|weak>           default "random"
  --time-limit <duration>                                         whole-session limit, e.g. 150m
  --question-time-limit <duration>                                per-question limit, e.g. 90s
  --image-viewer <sixel|xdg-open>                                  default "sixel"
  --answer                                                        reveal each answer/explanation immediately, no grading, no DB writes
  --dark                                                          invert question image colors, on by default (--dark=false for originals)
  --continue[=<id>]                                               resume a not-completed session exactly where it left off; bare --continue resumes the most recent not-completed one, or pass --continue=<id> for a specific one (see "itpec-sensei sessions --incomplete"); can't be combined with the pool flags above
  --repeat <id>                                                   start a new session reusing exam/topic/part/mode/order/limits from an existing session (completed or not), with a fresh draw; can't be combined with the pool flags above

Flags for history:
  --scope <all|topic:NAME|exam:ID|part:am|part:pm>   default "all"
  --order <newest|oldest>                             default "newest"
  --limit <n>                                        default 20, 0 = all
  --undo                                              delete the most recent attempt instead of listing history
  --session <id>                                      with --undo, only consider this session id, default 0 = most recent across all sessions

Flags for sessions:
  --scope <all|exam:ID|part:am|part:pm>   default "all" (topic scope not supported — a session isn't scoped to one topic)
  --order <newest|oldest>                 default "newest"
  --limit <n>                             default 20, 0 = all
  --incomplete                            only show sessions that never finished cleanly; ignores --scope/--order
  --delete <id>                           permanently delete this session and its attempts, instead of listing
  --yes                                   with --delete, skip the confirmation prompt

Flags for exam:
  itpec-sensei exam <examID>   positional exam id arg, e.g. 2025A_FE-A

Flags for reset:
  <all|topic:NAME|exam:ID|part:am|part:pm>   positional scope arg
  --yes                                       skip the confirmation prompt

Flags for data:
  --yes   skip the confirmation prompt before downloading

Flags for serve:
  --remote         expose over Streamable HTTP instead of stdio
  --addr           listen address for --remote      default "127.0.0.1:8790"
  --ngrok          also forward a public ngrok tunnel
  --image-viewer   local command the MCP open_question_image tool uses to open
                   images on the machine running the server (bypasses the MCP
                   client's own image rendering)                default "xdg-open"

Examples:
  itpec-sensei
  itpec-sensei --scope=exam:2025A_FE-A --period=week
  itpec-sensei practice --exam=2025A_FE-A
  itpec-sensei practice --exam-type=fe --part=pm --mode=review
  itpec-sensei practice --exam=2025A_FE-A --time-limit=150m --question-time-limit=90s
  itpec-sensei practice --exam=2025A_FE-A --q=34
  itpec-sensei practice --exam=2025A_FE-A --limit=5
  itpec-sensei practice --exam=2025A_FE-A --q=34 --answer
  itpec-sensei practice --topic="Networks" --part=am
  itpec-sensei exams
  itpec-sensei exam 2025A_FE-A
  itpec-sensei topics
  itpec-sensei sessions --incomplete
  itpec-sensei practice --continue
  itpec-sensei practice --continue=42
  itpec-sensei sessions --delete=42
  itpec-sensei practice --repeat=42
  itpec-sensei history --undo
  itpec-sensei reset exam:2025A_FE-A --yes
  itpec-sensei data
  itpec-sensei serve --remote --ngrok`)
}
