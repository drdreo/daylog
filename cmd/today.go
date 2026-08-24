package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/snapshot"
	"github.com/drdreo/daylog/internal/store"
	"github.com/drdreo/daylog/internal/view"
)

var (
	todayJSON    bool
	todayTypes   []string
	todaySources []string
)

var todayCmd = &cobra.Command{
	Use:   "today [DATE]",
	Short: "Show the folded view of a day (default: today)",
	Long: `Fold the event history into the day's derived state. --json emits the
consumer contract: {date, entries, open_todos, needs_triage} with
reclassifications, closures and triage verdicts already applied — consumers
stay dumb. open_todos holds every open todo; needs_triage filters it down to
the agent proposals still awaiting a verdict.

Filters: --type and --source narrow entries; --source matches either the
full identity (agent:claude) or the namespace (agent).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		date, err := argDate(args)
		if err != nil {
			return err
		}
		day, err := foldDay(date)
		if err != nil {
			return err
		}
		day.Entries = filterEntries(day.Entries, todayTypes, todaySources)

		if todayJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(day)
		}
		fmt.Print(view.Markdown(day))
		return nil
	},
}

func argDate(args []string) (time.Time, error) {
	if len(args) == 0 {
		return time.Now(), nil
	}
	d, err := time.ParseInLocation("2006-01-02", args[0], time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", args[0])
	}
	return d, nil
}

func foldDay(date time.Time) (view.Day, error) {
	all, err := store.ReadAll()
	if err != nil {
		return view.Day{}, err
	}
	day := view.Fold(all, date, time.Now())
	// The snapshot is a per-machine cache: unreadable or absent means the
	// view simply renders without live PR state, never fails (§4.4).
	if snap, err := snapshot.LoadGHPRs(); err == nil {
		view.JoinGH(&day, snap)
	}
	return day, nil
}

func filterEntries(entries []view.Entry, types, sources []string) []view.Entry {
	if len(types) == 0 && len(sources) == 0 {
		return entries
	}
	out := []view.Entry{}
	for _, e := range entries {
		if len(types) > 0 && !contains(types, e.Type) {
			continue
		}
		if len(sources) > 0 && !sourceMatch(sources, e.Source) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func sourceMatch(wanted []string, source string) bool {
	ns, _, _ := strings.Cut(source, ":")
	for _, w := range wanted {
		if w == source || w == ns {
			return true
		}
	}
	return false
}

func init() {
	todayCmd.Flags().BoolVar(&todayJSON, "json", false, "emit the folded view as JSON (the consumer contract)")
	todayCmd.Flags().StringArrayVar(&todayTypes, "type", nil, "only entries of this type; repeatable")
	todayCmd.Flags().StringArrayVar(&todaySources, "source", nil, "only entries from this source or namespace; repeatable")
	rootCmd.AddCommand(todayCmd)
}
