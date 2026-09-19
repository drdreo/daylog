package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/drdreo/daylog/internal/athena"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"time"
)

func printJSON(cmd *cobra.Command, v any) error {
	e := json.NewEncoder(cmd.OutOrStdout())
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func init() {
	rootCmd.AddCommand(&cobra.Command{Use: "prune", Short: "Remove aged processed evidence excerpts, never pending inputs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(""); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		q, err := capture.Open()
		if err != nil {
			return err
		}
		n, err := athena.Prune(q, cfg.EvidenceRetentionDays, time.Now())
		if err != nil {
			return err
		}
		return printJSON(cmd, map[string]int{"pruned_evidence": n})
	}})
	var dryRun, once bool
	c := &cobra.Command{Use: "curate --once", Short: "Curate queued reports; --dry-run previews without processing them", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if !once {
			return fmt.Errorf("--once required; schedule repeated one-shot runs")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		q, err := capture.Open()
		if err != nil {
			return err
		}
		w := athena.Worker{Queue: q, Config: cfg, Runner: athena.PiRunner{Config: cfg.Runner}}
		res, err := w.Once(cmd.Context(), dryRun)
		if e := printJSON(cmd, res); e != nil {
			return e
		}
		return err
	}}
	c.Flags().BoolVar(&once, "once", false, "perform one bounded run")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "preview decisions without changing receipts or the journal (uses model calls)")
	rootCmd.AddCommand(c)
	queue := &cobra.Command{Use: "queue", Short: "Inspect private candidates (not todos)"}
	var status string
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		q, e := capture.Open()
		if e != nil {
			return e
		}
		items, e := q.List(status)
		if e != nil {
			return e
		}
		return printJSON(cmd, items)
	}}
	list.Flags().StringVar(&status, "status", "", "pending|processing|processed|error")
	queue.AddCommand(list)
	var source string
	retry := &cobra.Command{Use: "retry <candidate-id>", Short: "Request fresh evaluation of a processed or failed report", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(source); err != nil {
			return err
		}
		q, err := capture.Open()
		if err != nil {
			return err
		}
		u, err := durable.Lock(filepath.Join(q.Root, "worker.lock"), false)
		if err != nil {
			return err
		}
		defer u()
		items, err := q.List("")
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.Candidate.ID == args[0] {
				if it.Receipt.Status == "processing" {
					return fmt.Errorf("unfinished plan must be recovered before retry")
				}
				r := capture.Receipt{Version: 2, CandidateID: it.Candidate.ID, Status: "pending", UpdatedAt: time.Now().Format(time.RFC3339Nano), Reason: "explicit human retry", AppliedEvents: it.Receipt.AppliedEvents}
				return q.SaveReceipt(r)
			}
		}
		return fmt.Errorf("unknown candidate")
	}}
	retry.Flags().StringVar(&source, "source", "", "human source override")
	queue.AddCommand(retry)
	rootCmd.AddCommand(queue)
	explain := &cobra.Command{Use: "explain <candidate-or-entry>", Short: "Inspect captured input, processing receipt, saved plan, or entry history", Long: `Inspect existing records without retrying or applying a plan.
For a candidate, captured_at is intake time and occurred_at is the reported
occurrence. receipt.updated_at is the latest receipt update, not necessarily a
model call or publication (pending can use capture time). plan.created_at is
planning time; planned events are not proof of publication. receipt.applied_events
and entry history identify recorded writes; a crash can leave acknowledgment
pending after a write. history[].recorded_at is the saved event timestamp, set at
planning time for Athena and preserved on replay, not exact delayed-publication
time. entry.display_at is the occurrence-based journal day.

Compare receipt.policy and plan.input.policy with daylog version's running policy.
An upgrade does not rewrite old receipts or re-evaluate saved plans. Compatible
live plans resume saved operations; incompatible plans stop for inspection and
explicit human retry. Saved shadow plans never auto-publish. Diagnostics do not
retry reports or reset budgets. Marker-only text ([sensitive line excluded])
means no prose remains in that excerpt, not that no work occurred; a truncated
excerpt cannot establish what the rest of the original content contained.`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		q, err := capture.Open()
		if err != nil {
			return err
		}
		items, err := q.List("")
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.Candidate.ID == args[0] {
				var plan any
				if it.Receipt.PlanID != "" {
					var p athena.Plan
					if err := durable.Read(filepath.Join(q.Root, "plans", it.Receipt.PlanID+".json"), &p); err != nil {
						return err
					}
					plan = p
				}
				return printJSON(cmd, map[string]any{"candidate": it.Candidate, "receipt": it.Receipt, "plan": plan})
			}
		}
		t, err := resolveTarget(args[0])
		if err != nil {
			return err
		}
		all, err := store.ReadAll()
		if err != nil {
			return err
		}
		history := []event.Event{}
		for _, e := range all {
			if e.ID == t.ID {
				history = append(history, e)
				continue
			}
			for _, target := range e.Targets {
				if target.ID == t.ID {
					history = append(history, e)
					break
				}
			}
		}
		return printJSON(cmd, map[string]any{"entry": t, "history": history})
	}}
	rootCmd.AddCommand(explain)
	rootCmd.AddCommand(&cobra.Command{Use: "version", Short: "Show this running binary's build, store/event versions, and compiled policy", Long: `Show versions compiled into this invocation, not a checkout or another installed
binary. Compare the exact executable used by your scheduled job with the one on
PATH. Upgrading does not rewrite history, replay held/skipped reports, or replace
saved plans; inspect explain and doctor before any explicit retry.`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return printJSON(cmd, map[string]any{"build": store.BuildVersion, "store": store.Version, "event": event.Version, "policy": athena.PolicyVersion})
	}})
	rootCmd.AddCommand(&cobra.Command{Use: "init", Short: "Initialize an empty versioned store; refuse unsupported data", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := store.Ensure(); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		p, _ := config.Path()
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return config.Save(cfg)
		}
		return nil
	}})
	var confirm bool
	repair := &cobra.Command{Use: "repair-tail DATE", Short: "Preserve damaged bytes and remove only a torn trailing record", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(""); err != nil {
			return err
		}
		if !confirm {
			return fmt.Errorf("--confirm required; original bytes are retained")
		}
		d, e := argDate(args)
		if e != nil {
			return e
		}
		return store.RepairTail(d)
	}}
	repair.Flags().BoolVar(&confirm, "confirm", false, "approve tail repair")
	rootCmd.AddCommand(repair)
}
