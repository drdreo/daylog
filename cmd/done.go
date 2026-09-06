package cmd

import (
	"fmt"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"time"
)

func init() {
	var source, note string
	c := &cobra.Command{Use: "done <entry>", Short: "Complete an adopted todo (human only)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := humanSource(source)
		if err != nil {
			return err
		}
		t, err := resolveTarget(args[0])
		if err != nil {
			return err
		}
		e := newEvent(time.Now(), s, event.TypeDone, note)
		e.Targets = []event.Target{{ID: t.ID, Revision: t.Revision}}
		if err := store.Append(e); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "completed", t.ID)
		return nil
	}}
	c.Flags().StringVar(&source, "source", "", "human source override")
	c.Flags().StringVarP(&note, "note", "n", "", "completion note")
	rootCmd.AddCommand(c)
}
