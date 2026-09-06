package cmd

import (
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	original "github.com/drdreo/daylog/internal/context"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"os"
	"strings"
	"time"
)

func init() {
	var typ, source, key string
	var refs []string
	c := &cobra.Command{Use: `add "report or note"`, Short: "Queue agent reports; directly log human notes and explicit todo proposals", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("DAYLOG_INTERNAL") == "1" {
			return fmt.Errorf("internal Athena capture excluded")
		}
		s, err := resolveSource(source)
		if err != nil {
			return err
		}
		if err := event.ValidateAddType(typ); err != nil {
			return err
		}
		ctx := captureCtx()
		root, err := store.DataDir()
		if err != nil {
			return err
		}
		if original.Within(ctx.Cwd, root) {
			return fmt.Errorf("capture from daylog data directory excluded")
		}
		norm := []string{}
		for _, r := range refs {
			n, e := event.NormalizeRef(r, ctx.Repository)
			if e != nil {
				return e
			}
			norm = append(norm, n)
		}
		now := time.Now()
		text := strings.TrimSpace(args[0])
		if !event.Human(s) && event.Narrative(typ) {
			q, e := capture.Open()
			if e != nil {
				return e
			}
			c, e := q.Enqueue(capture.Candidate{Version: capture.Version, Source: s, Kind: typ, Text: text, Refs: norm, CapturedAt: now.Format(time.RFC3339Nano), OccurredAt: now.Format(time.RFC3339Nano), TimeBasis: "report", Context: ctx, Origin: "report", IdempotencyKey: key, Completeness: "claim", Terminal: "unknown", Evidence: []string{}})
			if e != nil {
				return e
			}
			fmt.Fprintln(cmd.OutOrStdout(), "queued", c.ID)
			return nil
		}
		if key != "" {
			return fmt.Errorf("--idempotency-key applies to agent narrative intake only")
		}
		e := newEvent(now, s, typ, text)
		if !event.Human(s) {
			e.TimeBasis = "report"
		}
		e.Context = ctx
		e.Refs = norm
		if err := store.Append(e); err != nil {
			return err
		}
		verb := "logged"
		if typ == event.TypeTodo && !event.Human(s) {
			verb = "proposed"
		}
		fmt.Fprintln(cmd.OutOrStdout(), verb, e.ID)
		return nil
	}}
	c.Flags().StringVarP(&typ, "type", "t", event.TypeNote, "work|sidequest|note|todo")
	c.Flags().StringArrayVarP(&refs, "ref", "r", nil, "typed ref or #N in a repository; repeatable")
	c.Flags().StringVar(&source, "source", "", "source override (default DAYLOG_SOURCE, then human:cli)")
	c.Flags().StringVar(&key, "idempotency-key", "", "stable caller request key; reuse requires identical content")
	rootCmd.AddCommand(c)
}
