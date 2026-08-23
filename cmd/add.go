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
	addType   string
	addRefs   []string
	addSource string
)

var addCmd = &cobra.Command{
	Use:   `add "TLDR text"`,
	Short: "Append one entry to today's log",
	Long: `Append one immutable event. The CLI generates the id and timestamp,
captures cwd/repo/branch, normalizes refs, and validates before anything
touches disk. TLDRs over 280 chars are rejected, not truncated.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tldr := strings.TrimSpace(args[0])
		if err := event.ValidateTLDR(tldr); err != nil {
			return err
		}
		if err := event.ValidateAddType(addType); err != nil {
			return err
		}
		source, err := resolveSource(addSource)
		if err != nil {
			return err
		}

		ctx := captureCtx()
		refs := []string{}
		for _, r := range addRefs {
			norm, err := event.NormalizeRef(r, ctx.Repo)
			if err != nil {
				return err
			}
			refs = append(refs, norm)
		}

		e := newEvent(time.Now(), source, addType, tldr)
		e.Refs = refs
		e.Ctx = ctx
		if err := store.Append(e); err != nil {
			return err
		}
		fmt.Printf("logged %s %s: %s\n", e.Type, event.ShortID(e.ID), e.TLDR)
		return nil
	},
}

func init() {
	addCmd.Flags().StringVarP(&addType, "type", "t", event.TypeNote, "entry type: work|sidequest|note|todo")
	addCmd.Flags().StringArrayVarP(&addRefs, "ref", "r", nil, "typed ref (gh:pr:owner/repo#142, linear:ABC-123) or #N shorthand; repeatable")
	addCmd.Flags().StringVar(&addSource, "source", "", "producer identity override (default $DAYLOG_SOURCE, then human:cli)")
	rootCmd.AddCommand(addCmd)
}
