package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/poll"
)

var (
	pollDryRun bool
	pollOwners []string
)

var pollCmd = &cobra.Command{
	Use:   "poll <name>",
	Short: "Run a poller once (the same path the systemd timer uses)",
	Long: `Fetch current state from an external system, refresh its snapshot under
<data>/state/. A fetch failure keeps the previous snapshot and exits 0 — a
poller with no network is not an error state. Available pollers: gh.`,
}

var pollGHCmd = &cobra.Command{
	Use:   "gh",
	Short: "Refresh the snapshot of your open GitHub PRs",
	Long: `Track every open PR you authored via the gh CLI (which supplies auth).
The current snapshot goes to <data>/state/gh-prs.json and powers the separate
Open PRs section. PR lifecycle, checks, and review state never become work-log
events. Timer units to run this periodically are in docs/systemd/.

One machine is rarely one context, so --owner (repeatable, or a comma-
separated list) narrows the poll to certain repository owners; $DAYLOG_GH_OWNERS
sets the same thing per machine and the flag wins. A "gh_owners" key in
<data>/config.json is the third option, and the one that also applies to
polls launched outside a shell (a widget button, a scheduled job). A leading ! excludes an
owner, and @me stands for your own account:

  daylog poll gh --owner lovablelabs      # only the work org
  daylog poll gh --owner @me              # only your personal repos
  daylog poll gh --owner '!oldorg'        # everything but that one

PRs outside the filter are simply not tracked.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		owners, err := resolveGHOwners(cmd)
		if err != nil {
			return err
		}
		return poll.RunGH(os.Stdout, os.Stderr, pollDryRun, time.Now(), owners)
	},
}

// resolveGHOwners returns the owner filter spec: --owner beats
// $DAYLOG_GH_OWNERS beats config.json beats unfiltered — the flag for one
// run, the env var for one shell, the file for one machine. Precedence is
// keyed on whether a level was set, not on whether it is non-empty: an
// explicitly empty --owner or DAYLOG_GH_OWNERS means unfiltered rather
// than falling through to the file, which is the natural way to widen a
// single run back out to every owner.
func resolveGHOwners(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("owner") {
		return strings.Join(pollOwners, ","), nil
	}
	if spec, ok := os.LookupEnv("DAYLOG_GH_OWNERS"); ok {
		return spec, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return string(cfg.GHOwners), nil
}

var pollListErr = fmt.Errorf("unknown poller: available pollers are listed by `daylog poll --help`")

func init() {
	pollCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return pollListErr
		}
		return cmd.Help()
	}
	pollGHCmd.Flags().BoolVar(&pollDryRun, "dry-run", false, "show the refreshed snapshot size without writing it")
	pollGHCmd.Flags().StringSliceVar(&pollOwners, "owner", nil, "only track PRs under these repo owners (repeatable; !owner excludes, @me is you)")
	pollCmd.AddCommand(pollGHCmd)
	rootCmd.AddCommand(pollCmd)
}
