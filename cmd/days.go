package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/store"
	"github.com/drdreo/daylog/internal/view"
)

func init() {
	var asJSON bool
	command := &cobra.Command{
		Use:   "days [YYYY-MM]",
		Short: "List journal days and entry counts for a month (default: this month)",
		Long: `List nonempty days in the folded journal, including completed todos.
Open todos, dismissed/merged/declined entries, queue items and PR snapshots
are not counted. Dates follow the captured display day, just like today.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			month := time.Now()
			if len(args) == 1 {
				var err error
				month, err = time.ParseInLocation("2006-01", args[0], time.Local)
				if err != nil {
					return fmt.Errorf("invalid month %q: expected YYYY-MM", args[0])
				}
			}
			all, err := store.ReadAllExisting()
			if err != nil {
				return err
			}
			result := view.CalendarMonth(all, month)
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			for _, day := range result.Days {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d\n", day.Date, day.Count)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "emit the versioned monthly journal index")
	rootCmd.AddCommand(command)
}
