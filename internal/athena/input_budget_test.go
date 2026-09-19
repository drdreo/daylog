package athena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

func inputBytes(t *testing.T, in Input) int {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

func appendContextOutcome(t *testing.T, w *Worker, c capture.Candidate, n int, refs []string, episode string) event.Event {
	t.Helper()
	e := event.Event{Version: event.Version, ID: event.NewID(w.now()), PublicationKey: fmt.Sprintf("synthetic-context-%d", n), RecordedAt: w.now().Format(time.RFC3339Nano), OccurredAt: c.OccurredAt, TimeBasis: "report", Source: "agent:pi", Type: "work", TLDR: fmt.Sprintf("Synthetic outcome %d", n), Details: "Earlier behavior was checked; the latest change remains unverified.", Context: c.Context, Refs: refs, Provenance: &event.Provenance{Episode: episode}}
	if err := store.Append(e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLargeOptionalHistoryDoesNotBlockInput(t *testing.T) {
	w, _, c := fixture(t, "live")
	base, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	w.Config.Runner.MaxInputBytes = inputBytes(t, base) + 3500
	for i := 0; i < 80; i++ {
		appendContextOutcome(t, w, c, i, nil, fmt.Sprintf("other-%d", i))
	}
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatalf("optional history blocked current report: %v", err)
	}
	if inputBytes(t, in) > w.Config.Runner.MaxInputBytes || len(in.Outcomes) == 0 || len(in.Outcomes) >= 64 || in.Candidates[0].Text != c.Text {
		t.Fatalf("unbounded or missing input: %d bytes, %d outcomes", inputBytes(t, in), len(in.Outcomes))
	}
	if len(in.EditableTargets) != 0 {
		t.Fatal("unrelated history became editable", in.EditableTargets)
	}
}

func TestOversizedInputPreflightPreservesAllowance(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry=%v", dry), func(t *testing.T) {
			w, f, c := fixture(t, "live")
			w.Config.Runner.MaxInputBytes = 1024
			// Keep the cap valid while making required report context too large.
			c.ID = ""
			c.Text = strings.Repeat("synthetic report ", 200)
			var enqueueErr error
			c, enqueueErr = w.Queue.Enqueue(c)
			if enqueueErr != nil {
				t.Fatal(enqueueErr)
			}
			// Mark the small fixture report processed; only the large one is pending.
			items, listErr := w.Queue.List("pending")
			if listErr != nil {
				t.Fatal(listErr)
			}
			for _, it := range items {
				if it.Candidate.ID != c.ID {
					r := it.Receipt
					r.Status = "processed"
					r.Disposition = "outcome"
					if err := w.Queue.SaveReceipt(r); err != nil {
						t.Fatal(err)
					}
				}
			}
			res, err := w.Once(context.Background(), dry)
			if err == nil || !strings.Contains(err.Error(), "candidate "+c.ID+" cannot fit") {
				t.Fatalf("want individual preflight error, got %v", err)
			}
			if f.calls != 0 || res.Calls != 0 {
				t.Fatalf("local rejection called runner: %+v calls=%d", res, f.calls)
			}
			if _, err := os.Stat(filepath.Join(w.Queue.Root, "budget.json")); !os.IsNotExist(err) {
				t.Fatal("local rejection reserved budget", err)
			}
			items, err = w.Queue.List("")
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range items {
				if it.Candidate.ID != c.ID {
					continue
				}
				if dry && it.Receipt.Status != "pending" {
					t.Fatal("preview mutated receipt", it)
				}
				if !dry && (it.Receipt.Status != "error" || it.Receipt.PlanID != "") {
					t.Fatal("invalid preflight receipt", it)
				}
			}
		})
	}
}

func TestSuccessfulInvocationsObeyRunAndDailyLimits(t *testing.T) {
	w, f, c := fixture(t, "live")
	w.Config.Runner.CallsPerRun = 1
	w.Config.Runner.CallsPerDay = 2
	for _, task := range []string{"second", "third"} {
		c.ID, c.Context.Task = "", task
		if _, err := w.Queue.Enqueue(c); err != nil {
			t.Fatal(err)
		}
	}
	for calls := 1; calls <= 2; calls++ {
		res, err := w.Once(context.Background(), false)
		if err != nil || res.Calls != 1 || res.Events != 1 || f.calls != calls {
			t.Fatal("per-run allowance violated", res, err, f.calls)
		}
	}
	res, err := w.Once(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "daily model call limit reached") || res.Calls != 0 || f.calls != 2 {
		t.Fatal("daily allowance violated", res, err, f.calls)
	}
	pending, err := w.Queue.List("pending")
	if err != nil || len(pending) != 1 || pending[0].Receipt.Attempts != 0 || pending[0].Receipt.PlanID != "" {
		t.Fatal("daily cap changed pending receipt", pending, err)
	}
}

func TestAttemptedRunnerFailureConsumesAllowance(t *testing.T) {
	w, f, c := fixture(t, "live")
	w.Config.Runner.CallsPerRun = 1
	w.Config.Runner.CallsPerDay = 1
	other := c
	other.ID, other.Context.Task = "", "other-episode"
	if _, err := w.Queue.Enqueue(other); err != nil {
		t.Fatal(err)
	}
	f.fn = func(Input) (Output, error) {
		var budget Budget
		if err := durable.Read(filepath.Join(w.Queue.Root, "budget.json"), &budget); err != nil || budget.Calls != 1 {
			return Output{}, fmt.Errorf("reservation not durable before invocation: %+v, %v", budget, err)
		}
		return Output{}, errors.New("synthetic provider failure")
	}
	res, err := w.Once(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "synthetic provider failure") || res.Calls != 1 || f.calls != 1 {
		t.Fatal(res, err, f.calls)
	}
	res, err = w.Once(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "daily model call limit reached") || res.Calls != 0 || f.calls != 1 {
		t.Fatal("attempt was refunded or per-run/daily cap ignored", res, err, f.calls)
	}
}

func TestConcurrentWorkersCannotDoubleReserveBudget(t *testing.T) {
	w, f, _ := fixture(t, "live")
	w.Config.Runner.CallsPerDay = 1
	entered, release := make(chan struct{}), make(chan struct{})
	f.fn = func(in Input) (Output, error) {
		close(entered)
		<-release
		return Output{Version: Version, Actions: []Action{{Kind: "skip", Candidates: []string{in.Candidates[0].ID}, Reason: "synthetic duplicate"}}}, nil
	}
	second := *w
	secondRunner := &fakeRunner{fn: func(Input) (Output, error) { return Output{}, errors.New("must not run concurrently") }}
	second.Runner = secondRunner
	done := make(chan error, 1)
	go func() { _, err := w.Once(context.Background(), false); done <- err }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("worker did not enter runner")
	}
	res, err := second.Once(context.Background(), false)
	close(release)
	if firstErr := <-done; firstErr != nil {
		t.Fatal(firstErr)
	}
	if err == nil || res.Calls != 0 || secondRunner.calls != 0 {
		t.Fatal("concurrent worker bypassed lock", res, err, secondRunner.calls)
	}
	var budget Budget
	if err := durable.Read(filepath.Join(w.Queue.Root, "budget.json"), &budget); err != nil || budget.Calls != 1 {
		t.Fatal(budget, err)
	}
}

func TestLocalPreflightLeavesExistingBudgetBytesUntouched(t *testing.T) {
	w, _, _ := fixture(t, "live")
	w.Config.Runner.MaxInputBytes = 1
	path := filepath.Join(w.Queue.Root, "budget.json")
	// Even an earlier day's persisted budget must not be reset by preflight.
	budget := Budget{Version: Version, Date: "2026-09-05", Calls: 17}
	if err := durable.JSON(path, budget); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Once(context.Background(), false); err == nil {
		t.Fatal("oversized input accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("preflight changed durable budget", err)
	}
}

func TestUnfitCandidateDoesNotBlockSameEpisodeOrOtherReports(t *testing.T) {
	w, f, c := fixture(t, "live")
	large := c
	large.ID = ""
	large.Text = strings.Repeat("oversized claim ", 500)
	large.CapturedAt = w.now().Add(-20 * time.Minute).Format(time.RFC3339)
	bad, err := w.Queue.Enqueue(large)
	if err != nil {
		t.Fatal(err)
	}
	other := c
	other.ID = ""
	other.Context.Task = "unrelated"
	if _, err := w.Queue.Enqueue(other); err != nil {
		t.Fatal(err)
	}
	w.Config.Runner.MaxInputBytes = 4000
	w.Config.Runner.CallsPerRun = 2
	w.Config.Runner.CallsPerDay = 2
	res, err := w.Once(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "candidate "+bad.ID+" cannot fit") {
		t.Fatalf("unfit error missing: %v", err)
	}
	if f.calls != 2 || res.Calls != 2 || res.Events != 2 {
		t.Fatalf("processable reports blocked: %+v calls=%d", res, f.calls)
	}
	var budget Budget
	if err := durable.Read(filepath.Join(w.Queue.Root, "budget.json"), &budget); err != nil || budget.Calls != 2 {
		t.Fatal(budget, err)
	}
	items, err := w.Queue.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Candidate.ID == bad.ID {
			if it.Receipt.Status != "error" || it.Receipt.PlanID != "" {
				t.Fatal(it.Receipt)
			}
		} else if it.Receipt.Status != "processed" || it.Receipt.PlanID == "" {
			t.Fatal(it.Receipt)
		}
	}
}
