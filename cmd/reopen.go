package cmd

import (
	"fmt"
	"time"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
)

func init() {
	var source string
	c := &cobra.Command{Use: "reopen <entry>", Short: "Undo todo completion (human only)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := humanSource(source)
		if err != nil {
			return err
		}
		t, err := resolveTarget(args[0])
		if err != nil {
			return err
		}
		e := newEvent(time.Now(), s, event.TypeReopen, "")
		e.Targets = []event.Target{{ID: t.ID, Revision: t.Revision}}
		if err := store.Append(e); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "reopened", t.ID)
		return nil
	}}
	c.Flags().StringVar(&source, "source", "", "human source override")
	rootCmd.AddCommand(c)
}
