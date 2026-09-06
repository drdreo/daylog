package cmd

import (
	"fmt"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"time"
)

func init() {
	for _, spec := range []struct{ name, verdict string }{{"accept", event.VerdictAccepted}, {"decline", event.VerdictDeclined}} {
		var source, note string
		c := &cobra.Command{Use: spec.name + " <entry>", Short: "Human todo triage", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := humanSource(source)
			if err != nil {
				return err
			}
			t, err := resolveTarget(args[0])
			if err != nil {
				return err
			}
			e := newEvent(time.Now(), s, event.TypeTriage, "")
			e.Targets = []event.Target{{ID: t.ID, Revision: t.Revision}}
			e.Verdict = spec.verdict
			e.Reason = note
			if err := store.Append(e); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), spec.verdict, t.ID)
			return nil
		}}
		c.Flags().StringVar(&source, "source", "", "human source override")
		c.Flags().StringVarP(&note, "note", "n", "", "reason")
		rootCmd.AddCommand(c)
	}
}
