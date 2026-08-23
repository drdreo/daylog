package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/poll"
)

var pollDryRun bool

var pollCmd = &cobra.Command{
	Use:   "poll <name>",
	Short: "Run a poller once (the same path the systemd timer uses)",
	Long: `Fetch current state from an external system, refresh its snapshot under
<data>/state/, and log transition events for meaningful changes (§6).
A fetch failure keeps the previous snapshot and exits 0 — a poller with
no network is not an error state. Available pollers: gh.`,
}

var pollGHCmd = &cobra.Command{
	Use:   "gh",
	Short: "Poll GitHub for your open PRs (merges, check flips, reviews)",
	Long: `Track every open PR you authored via the gh CLI (which supplies auth).
Snapshot goes to <data>/state/gh-prs.json; transitions are logged for
PRs merged or closed, checks flipping red/green, and review decisions.
The first run only establishes the baseline. Timer units to run this
periodically are in docs/systemd/.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return poll.RunGH(os.Stdout, os.Stderr, pollDryRun, time.Now())
	},
}

var pollListErr = fmt.Errorf("unknown poller: available pollers are listed by `daylog poll --help`")

func init() {
	pollCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return pollListErr
		}
		return cmd.Help()
	}
	pollGHCmd.Flags().BoolVar(&pollDryRun, "dry-run", false, "print transitions without writing events or the snapshot")
	pollCmd.AddCommand(pollGHCmd)
	rootCmd.AddCommand(pollCmd)
}
