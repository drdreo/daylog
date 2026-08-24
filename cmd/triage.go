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
	acceptSource  string
	declineSource string
	declineNote   string
)

var acceptCmd = &cobra.Command{
	Use:   "accept <id-prefix|tldr-substring>",
	Short: "Adopt an agent-filed todo as your own",
	Long: `Append a triage event marking an agent's proposal as accepted. The todo
keeps its type and stays in your open todos — accepting only clears the
"awaiting triage" flag, so the widget stops nagging about it.

The opposite is ` + "`daylog decline`" + `. Both are append-only: a later verdict
reverses an earlier one.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return triage(args[0], event.VerdictAccepted, acceptSource, "")
	},
}

var declineCmd = &cobra.Command{
	Use:   "decline <id-prefix|tldr-substring>",
	Short: "Reject an agent-filed todo; it stops rendering anywhere",
	Long: `Append a triage event marking an agent's proposal as declined. The event
stays in the ledger — nothing is ever deleted — but the todo drops out of
every rendered view: it was never yours to carry.

Use --note to record why. To take it back, run ` + "`daylog accept`" + ` on the
same entry.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return triage(args[0], event.VerdictDeclined, declineSource, declineNote)
	},
}

// triage appends one verdict event against an existing todo. Guarding on
// the effective type keeps the verdict meaningful: only a proposal can be
// accepted or declined.
func triage(target, verdict, sourceFlag, note string) error {
	e2, err := resolveTarget(target)
	if err != nil {
		return err
	}
	if e2.Type != event.TypeTodo {
		return fmt.Errorf("entry %s is type %q, not a todo: only todos are triaged", event.ShortID(e2.ID), e2.Type)
	}
	source, err := resolveSource(sourceFlag)
	if err != nil {
		return err
	}
	// A proposal is the human's to rule on: an agent that could accept its
	// own todo would make the review queue ceremonial (§5.2). This is also
	// the common accident — an agent's $DAYLOG_SOURCE leaking into a shell
	// the human is driving.
	if strings.HasPrefix(source, "agent:") {
		verb := "accept"
		if verdict == event.VerdictDeclined {
			verb = "decline"
		}
		return fmt.Errorf("triage is the human's call: %s cannot %s a proposal (unset $DAYLOG_SOURCE or pass --source human:<name>)", source, verb)
	}

	tldr := fmt.Sprintf("%s: %s", verdict, e2.TLDR)
	if note = strings.TrimSpace(note); note != "" {
		tldr = fmt.Sprintf("%s (%s): %s", verdict, note, e2.TLDR)
	}
	if err := event.ValidateTLDR(tldr); err != nil {
		return err
	}

	ev := newEvent(time.Now(), source, event.TypeTriage, tldr)
	ev.Parent = &e2.ID
	ev.Meta["verdict"] = verdict
	ev.Ctx = captureCtx()
	if err := store.Append(ev); err != nil {
		return err
	}
	fmt.Printf("%s %s: %s\n", verdict, event.ShortID(e2.ID), e2.TLDR)
	return nil
}

func init() {
	acceptCmd.Flags().StringVar(&acceptSource, "source", "", "producer identity override (default $DAYLOG_SOURCE, then human:cli)")
	declineCmd.Flags().StringVarP(&declineNote, "note", "n", "", "why it was declined")
	declineCmd.Flags().StringVar(&declineSource, "source", "", "producer identity override (default $DAYLOG_SOURCE, then human:cli)")
	rootCmd.AddCommand(acceptCmd)
	rootCmd.AddCommand(declineCmd)
}
