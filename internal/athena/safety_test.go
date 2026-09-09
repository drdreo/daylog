package athena

import (
	"context"
	"errors"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"testing"
	"time"
)

func TestMidPlanRecoveryAndDryRun(t *testing.T) {
	w, f, c := fixture(t, "live")
	f.fn = func(Input) (Output, error) {
		return Output{Version: 2, Actions: []Action{{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "First outcome", Reason: "distinct change"}, {Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "Second outcome", Reason: "separate diagnosis"}}}, nil
	}
	w.AfterAppend = func() error { return errors.New("crash") }
	w.Once(context.Background(), false)
	w.AfterAppend = nil
	if _, e := w.Once(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	all, _ := store.ReadAll()
	if len(all) != 1 {
		t.Fatal("dry-run resumed publication")
	}
	if _, e := w.Once(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	all, _ = store.ReadAll()
	if len(all) != 2 || f.calls != 1 {
		t.Fatal(len(all), f.calls)
	}
}
func TestHumanEditWhileModelRunsWins(t *testing.T) {
	w, f, c := fixture(t, "live")
	if _, e := w.Once(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	all, _ := store.ReadAll()
	original := all[0]
	c.ID = ""
	c.Text = "New evidence for existing outcome"
	w.Queue.Enqueue(c)
	f.fn = func(in Input) (Output, error) {
		now := time.Now()
		e := event.Event{Version: 2, ID: event.NewID(now), PublicationKey: "human-race", Source: "human:cli", Type: "amend", TLDR: "Human wording wins", RecordedAt: now.Format(time.RFC3339Nano), OccurredAt: now.Format(time.RFC3339Nano), TimeBasis: "human", Targets: []event.Target{{ID: original.ID, Revision: 1}}}
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
		return Output{Version: 2, Actions: []Action{{Kind: "amend", Candidates: []string{in.Candidates[0].ID}, Targets: []event.Target{{ID: original.ID, Revision: 1}}, Type: "work", Text: "Stale model wording", Reason: "new input"}}}, nil
	}
	if _, e := w.Once(context.Background(), false); e == nil {
		t.Fatal("stale edit applied")
	}
	all, _ = store.ReadAll()
	effective := event.Effective(all)
	if effective[original.ID].TLDR != "Human wording wins" || !effective[original.ID].Pinned {
		t.Fatal(effective)
	}
	items, _ := w.Queue.List("error")
	if len(items) != 1 {
		t.Fatal(items)
	}
}
func TestRetentionKeepsPendingEvidence(t *testing.T) {
	w, f, c := fixture(t, "live")
	e, err := w.Queue.PutEvidence("bounded claim", "assistant-claim", c.CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = ""
	c.Evidence = []string{e.ID}
	c.Text = "Native boundary"
	queued, err := w.Queue.Enqueue(c)
	if err != nil {
		t.Fatal(err)
	}
	old := w.now().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	if err := w.Queue.SaveReceipt(capture.Receipt{Version: 2, CandidateID: queued.ID, Status: "processed", Disposition: "skip", UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	other := c
	other.ID = ""
	other.Context.Task = "other"
	other.Text = "pending"
	pending, err := w.Queue.Enqueue(other)
	if err != nil {
		t.Fatal(err)
	}
	if n, e := Prune(w.Queue, 14, w.now()); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	w.Queue.SaveReceipt(capture.Receipt{Version: 2, CandidateID: pending.ID, Status: "processed", Disposition: "skip", UpdatedAt: old})
	if n, err := Prune(w.Queue, 14, w.now()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := w.Queue.GetEvidence(e.ID); err == nil {
		t.Fatal("aged evidence retained")
	}
	if f.calls != 0 {
		t.Fatal("pruning called model")
	}
}
