package cmd

import (
	"fmt"
	"github.com/drdreo/daylog/internal/athena"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"path/filepath"
	"time"
)

func init() {
	for _, kind := range []string{event.TypeAmend, event.TypeDismiss, event.TypeRestore, event.TypeMerge} {
		var source, reason, typ string
		c := &cobra.Command{Use: kind + " <entry> [wording or merge-targets]", Short: "Append a human narrative correction", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := humanSource(source)
			if err != nil {
				return err
			}
			target, err := resolveTarget(args[0])
			if err != nil {
				return err
			}
			e := newEvent(time.Now(), s, kind, "")
			e.Targets = []event.Target{{ID: target.ID, Revision: target.Revision}}
			e.Reason = reason
			e.ToType = typ
			switch kind {
			case event.TypeAmend:
				if len(args) != 2 {
					return fmt.Errorf("amend <entry> \"wording\" [--type work|sidequest|note]")
				}
				e.TLDR = args[1]
			case event.TypeMerge:
				if len(args) < 3 {
					return fmt.Errorf("merge <primary> <duplicate>... \"wording\"")
				}
				e.TLDR = args[len(args)-1]
				for _, arg := range args[1 : len(args)-1] {
					other, err := resolveTarget(arg)
					if err != nil {
						return err
					}
					e.Targets = append(e.Targets, event.Target{ID: other.ID, Revision: other.Revision})
				}
			default:
				if len(args) != 1 {
					return fmt.Errorf("expected one entry")
				}
			}
			if err := store.Append(e); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), kind, target.ID)
			return nil
		}}
		c.Flags().StringVar(&source, "source", "", "human source override")
		c.Flags().StringVar(&reason, "reason", "", "reason for correction")
		if kind == event.TypeAmend {
			c.Flags().StringVar(&typ, "type", "", "new narrative type")
		}
		rootCmd.AddCommand(c)
	}
	var source, reason string
	pref := &cobra.Command{Use: "prefer <entry> <keep|skip|duplicate>", Short: "Save a private editorial preference example", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(source); err != nil {
			return err
		}
		if args[1] != "keep" && args[1] != "skip" && args[1] != "duplicate" {
			return fmt.Errorf("invalid preference")
		}
		if len(reason) > 1024 {
			return fmt.Errorf("reason too long")
		}
		t, err := resolveTarget(args[0])
		if err != nil {
			return err
		}
		q, err := capture.Open()
		if err != nil {
			return err
		}
		u, err := durable.Lock(filepath.Join(q.Root, "preferences.lock"), true)
		if err != nil {
			return err
		}
		defer u()
		p := filepath.Join(q.Root, "preferences.json")
		data, err := athena.LoadPreferences(p)
		if err != nil {
			return err
		}
		data.Examples = append(data.Examples, athena.Preference{Entry: t.ID, Choice: args[1], Reason: reason})
		if len(data.Examples) > 100 {
			data.Examples = data.Examples[len(data.Examples)-100:]
		}
		return durable.JSON(p, data)
	}}
	pref.Flags().StringVar(&source, "source", "", "human source override")
	pref.Flags().StringVar(&reason, "reason", "", "editorial example explanation")
	rootCmd.AddCommand(pref)
}
