package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

var reclassifySource string

var reclassifyCmd = &cobra.Command{
	Use:   "reclassify <id-prefix|tldr-substring> <new-type>",
	Short: "Reinterpret an earlier entry without editing history",
	Long: `Append a reclassify event: the target keeps its original line, but the
folded view shows the new type. This is how a side quest gets promoted
to real work, or how an agent todo is adopted as yours.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := resolveTarget(args[0])
		if err != nil {
			return err
		}
		newType := args[1]
		if err := event.ValidateAddType(newType); err != nil {
			return err
		}
		if newType == target.Type {
			return fmt.Errorf("entry %s is already type %q", event.ShortID(target.ID), newType)
		}
		source, err := resolveSource(reclassifySource)
		if err != nil {
			return err
		}

		e := newEvent(time.Now(), source, event.TypeReclassify,
			fmt.Sprintf("reclassified %s → %s: %s", target.Type, newType, target.TLDR))
		e.Parent = &target.ID
		e.Meta["to"] = newType
		e.Ctx = captureCtx()
		if err := store.Append(e); err != nil {
			return err
		}
		fmt.Printf("reclassified %s to %s: %s\n", event.ShortID(target.ID), newType, target.TLDR)
		return nil
	},
}

func init() {
	reclassifyCmd.Flags().StringVar(&reclassifySource, "source", "", "producer identity override (default $DAYLOG_SOURCE, then human:cli)")
	rootCmd.AddCommand(reclassifyCmd)
}
