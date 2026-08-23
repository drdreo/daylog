package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

var (
	doneNote   string
	doneSource string
)

var doneCmd = &cobra.Command{
	Use:   "done <id-prefix|tldr-substring>",
	Short: "Close a todo (or mark any entry closed)",
	Long: `Append a done event pointing at an earlier entry. The target is a
unique ULID prefix or a case-insensitive substring of the tldr; open
todos are searched first, then recent entries. Ambiguity is an error.

Use --note for a closing remark, e.g. a won't-do reason when dismissing
an agent-filed todo from the review queue.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := resolveTarget(args[0])
		if err != nil {
			return err
		}
		source, err := resolveSource(doneSource)
		if err != nil {
			return err
		}
		note := strings.TrimSpace(doneNote)
		if note == "" {
			note = "done: " + target.TLDR
		}
		if err := event.ValidateTLDR(note); err != nil {
			return err
		}

		e := newEvent(time.Now(), source, event.TypeDone, note)
		e.Parent = &target.ID
		e.Ctx = captureCtx()
		if err := store.Append(e); err != nil {
			return err
		}
		fmt.Printf("closed %s: %s\n", event.ShortID(target.ID), target.TLDR)
		return nil
	},
}

func init() {
	doneCmd.Flags().StringVarP(&doneNote, "note", "n", "", "closing note (e.g. a won't-do reason)")
	doneCmd.Flags().StringVar(&doneSource, "source", "", "producer identity override (default $DAYLOG_SOURCE, then human:cli)")
	rootCmd.AddCommand(doneCmd)
}
