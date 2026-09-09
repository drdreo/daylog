package athena

import (
	"context"
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/store"
)

func TestPolicyChangeStopsSavedPlanWithoutBlockingOtherReports(t *testing.T) {
	for _, progress := range []string{"not-started", "acknowledged", "append-before-ack"} {
		t.Run(progress, func(t *testing.T) {
			w, f, c := fixture(t, "live")
			in, err := w.input([]capture.Item{{Candidate: c}})
			if err != nil {
				t.Fatal(err)
			}
			in.Policy = "athena-v2.1"
			out := Output{Version: 2, Actions: []Action{
				{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "First outcome", Reason: "change"},
				{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "Second outcome", Reason: "finding"},
			}}
			plan := w.makePlan("live", in, out, "old-policy-plan")
			if progress != "not-started" {
				id, err := store.AppendOnce(*plan.Operations[0].Event)
				if err != nil {
					t.Fatal(err)
				}
				if progress == "acknowledged" {
					plan.Applied[plan.Operations[0].ID] = id
				}
			}
			if err := durable.JSON(w.planPath(plan.ID), plan); err != nil {
				t.Fatal(err)
			}
			r := capture.Receipt{Version: 2, CandidateID: c.ID, Status: "processing", PlanID: plan.ID, UpdatedAt: c.CapturedAt}
			if err := w.Queue.SaveReceipt(r); err != nil {
				t.Fatal(err)
			}
			other := c
			other.ID, other.Context.Task, other.Text = "", "other-task", "An unrelated completed task"
			if _, err := w.Queue.Enqueue(other); err != nil {
				t.Fatal(err)
			}
			res, err := w.Once(context.Background(), false)
			if err == nil || !strings.Contains(err.Error(), "policy") || res.Events != 1 || f.calls != 1 {
				t.Fatal("incompatible plan blocked unrelated work", res, f.calls, err)
			}
			var saved Plan
			if err := durable.Read(w.planPath(plan.ID), &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Status != "stale" || len(saved.Operations) != 2 {
				t.Fatal("plan was not retained in a stopped state", saved.Status)
			}
			items, _ := w.Queue.List("error")
			if len(items) != 1 || items[0].Candidate.ID != c.ID || items[0].Receipt.PlanID != plan.ID {
				t.Fatal("receipt must leave processing and permit explicit CLI retry", items)
			}
			wantApplied := 0
			if progress != "not-started" {
				wantApplied = 1
			}
			if len(saved.Applied) != wantApplied || len(items[0].Receipt.AppliedEvents) != wantApplied {
				t.Fatal("lost already-published operation", saved.Applied, items[0].Receipt.AppliedEvents)
			}
			if _, err := w.Once(context.Background(), false); err != nil {
				t.Fatal("stopped plan kept failing subsequent ticks", err)
			}
			all, err := store.ReadAllExisting()
			if err != nil || len(all) != wantApplied+1 || f.calls != 1 {
				t.Fatal("duplicate publication", len(all), f.calls, err)
			}
		})
	}
}
