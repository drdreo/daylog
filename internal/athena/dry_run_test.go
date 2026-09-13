package athena

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/store"
)

func TestDryRunLeavesReportsPendingForLiveEvaluation(t *testing.T) {
	w, f, _ := fixture(t, "live")
	before, err := w.Queue.List("")
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Once(context.Background(), true)
	if err != nil || res.Mode != "dry-run" || res.Events != 0 || res.Plans != 0 || len(res.Preview) != 1 {
		t.Fatal(res, err)
	}
	after, err := w.Queue.List("")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preview changed candidates or receipts", err)
	}
	plans, err := filepath.Glob(filepath.Join(w.Queue.Root, "plans", "*.json"))
	if err != nil || len(plans) != 0 {
		t.Fatal("preview persisted a publication plan", plans, err)
	}
	all, err := store.ReadAll()
	if err != nil || len(all) != 0 {
		t.Fatal("preview published", all, err)
	}
	var budget Budget
	if err := durable.Read(filepath.Join(w.Queue.Root, "budget.json"), &budget); err != nil || budget.Calls != 1 {
		t.Fatal("preview did not count its model call", budget, err)
	}
	res, err = w.Once(context.Background(), false)
	if err != nil || res.Events != 1 || f.calls != 2 {
		t.Fatal("live must freshly evaluate previewed input without manual retry", res, f.calls, err)
	}
}

func TestDryRunFailuresDoNotConsumeAttempts(t *testing.T) {
	for _, failure := range []string{"model", "invalid-decision"} {
		t.Run(failure, func(t *testing.T) {
			w, f, _ := fixture(t, "live")
			switch failure {
			case "model":
				f.fn = func(Input) (Output, error) { return Output{}, errors.New("unavailable") }
			case "invalid-decision":
				f.fn = func(Input) (Output, error) { return Output{}, nil }
			}
			before, _ := w.Queue.List("")
			if _, err := w.Once(context.Background(), true); err == nil {
				t.Fatal("failure not reported")
			}
			after, err := w.Queue.List("")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("preview failure changed receipt", err)
			}
		})
	}
}

func TestLegacyShadowConfigurationRequiresExplicitResume(t *testing.T) {
	w, f, _ := fixture(t, "shadow")
	if _, err := w.Once(context.Background(), false); err == nil || !strings.Contains(err.Error(), "setup --mode live") {
		t.Fatal("legacy shadow config silently enabled", err)
	}
	if f.calls != 0 {
		t.Fatal("paused worker called model")
	}
	if res, err := w.Once(context.Background(), true); err != nil || len(res.Preview) != 1 {
		t.Fatal("explicit preview should still work", res, err)
	}
}

func TestLegacyShadowPlansNeverAutoPublish(t *testing.T) {
	for _, status := range []string{"ready", "shadow"} {
		t.Run(status, func(t *testing.T) {
			w, f, c := fixture(t, "live")
			in, err := w.input([]capture.Item{{Candidate: c}})
			if err != nil {
				t.Fatal(err)
			}
			out, _ := f.fn(in)
			plan := w.makePlan("shadow", in, out, "legacy-shadow")
			plan.Status = status
			if err := durable.JSON(w.planPath(plan.ID), plan); err != nil {
				t.Fatal(err)
			}
			r := capture.Receipt{Version: 2, CandidateID: c.ID, Status: "processing", PlanID: plan.ID, UpdatedAt: c.CapturedAt}
			if err := w.Queue.SaveReceipt(r); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Once(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			all, err := store.ReadAll()
			if err != nil || len(all) != 0 || f.calls != 0 {
				t.Fatal("legacy preview was published or re-evaluated automatically", all, f.calls, err)
			}
			items, err := w.Queue.List("processed")
			if err != nil || len(items) != 1 {
				t.Fatal("interrupted legacy preview receipt was not finalized", items, err)
			}
		})
	}
}
