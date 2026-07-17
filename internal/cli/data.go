package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// RunData implements `itpec-sensei data [--yes]`: reports the installed data
// version, checks GitHub for a newer release, and offers to download it.
func RunData(ctx context.Context, dataDir string, args []string) error {
	fs := flag.NewFlagSet("data", flag.ExitOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}

	current, installed := core.InstalledVersion(dataDir)
	if installed {
		fmt.Printf("Installed data version: %s\n", current)
	} else {
		fmt.Println("Question data is not installed.")
	}

	fmt.Println("Checking github.com/jim-ww/itpec-sensei for the latest release...")
	_, latest, hasUpdate, err := core.CheckUpdate(ctx, dataDir)
	if err != nil {
		return fmt.Errorf("check for update: %w", err)
	}

	if installed && !hasUpdate {
		fmt.Println("Already up to date.")
		return nil
	}

	if installed {
		fmt.Printf("Update available: %s -> %s\n", current, latest)
	} else {
		fmt.Printf("Latest available version: %s\n", latest)
	}
	return promptAndDownload(ctx, dataDir, latest, *yes, false)
}

// confirm prints prompt and reports whether the user answered y/yes.
func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// promptAndDownload asks for consent (unless yes is set) and downloads the
// given release tag's data archive into dataDir on confirmation. If
// exitOnDecline is set, declining prints how to install later and exits(1) —
// used for the first-run gate in main.go, where the requested command cannot
// proceed without data. Otherwise declining is a no-op (used by the explicit
// "data" command, which is just a voluntary check).
func promptAndDownload(ctx context.Context, dataDir, tag string, yes, exitOnDecline bool) error {
	if !yes && !confirm(fmt.Sprintf("Download question data %s (~350MB) from github.com/jim-ww/itpec-sensei releases? [y/N] ", tag)) {
		if exitOnDecline {
			fmt.Println("Declined. Run \"itpec-sensei data --yes\" whenever you're ready to install it.")
			os.Exit(1)
		}
		fmt.Println("Skipped.")
		return nil
	}

	_, assetURL, err := core.LatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("resolve download URL: %w", err)
	}
	fmt.Println("Downloading...")
	if err := core.DownloadAndInstall(ctx, dataDir, tag, assetURL); err != nil {
		return fmt.Errorf("install data: %w", err)
	}
	fmt.Printf("Installed data version %s.\n", tag)
	return nil
}

// EnsureData makes sure question data is installed before any command that
// needs the bank runs. If it's missing, it only prompts when stdin is a real
// terminal — "itpec-sensei serve" (stdio MCP mode) uses stdin as the MCP
// transport, so reading a confirmation line from it would corrupt the
// protocol stream, and other non-interactive invocations have no user to ask.
// In those cases it fails fast with instructions instead of blocking on a
// prompt nobody can answer.
func EnsureData(ctx context.Context, dataDir string) error {
	if core.Installed(dataDir) {
		return nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("question data is not installed; run \"itpec-sensei data --yes\" in a terminal first")
	}

	fmt.Println("itpec-sensei needs to download the question bank from github.com/jim-ww/itpec-sensei before it can run.")
	if !confirm("Download question data (~350MB) from github.com/jim-ww/itpec-sensei releases? [y/N] ") {
		fmt.Println("Declined. Run \"itpec-sensei data --yes\" whenever you're ready to install it.")
		os.Exit(1)
	}

	tag, assetURL, err := core.LatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("resolve download URL: %w", err)
	}
	fmt.Println("Downloading...")
	if err := core.DownloadAndInstall(ctx, dataDir, tag, assetURL); err != nil {
		return fmt.Errorf("install data: %w", err)
	}
	fmt.Printf("Installed data version %s.\n", tag)
	return nil
}
