package athena

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

func TestNearCapKeepsReportsEvidenceAndRequiredContextWhole(t *testing.T) {
	w, _, c := fixture(t, "live")
	c.Refs = []string{"linear:TEST-9"}
	c.Text = "Implemented the earlier part. Later integration remains blocked. <>&"
	evidence, err := w.Queue.PutEvidence("Synthetic claimed evidence with an unresolved limitation.", "assistant-claim", c.CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	c.Evidence = []string{evidence.ID}
	linked := appendContextOutcome(t, w, c, 0, c.Refs, "other-turn")
	mandatory, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	exact := inputBytes(t, mandatory)
	w.Config.Runner.MaxInputBytes = exact
	for i := 1; i <= 70; i++ {
		appendContextOutcome(t, w, c, i, nil, fmt.Sprintf("unrelated-%d", i))
	}
	prefs := struct {
		Version  int          `json:"version"`
		Examples []Preference `json:"examples"`
	}{Version: Version, Examples: []Preference{{Entry: linked.ID, Choice: "skip", Reason: strings.Repeat("optional preference ", 20)}}}
	if err := durable.JSON(filepath.Join(w.Queue.Root, "preferences.json"), prefs); err != nil {
		t.Fatal(err)
	}
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	if inputBytes(t, in) != exact || !reflect.DeepEqual(in, mandatory) {
		t.Fatalf("required facts changed at cap: %+v", in)
	}
	if len(in.EditableTargets) != 1 || in.EditableTargets[0].ID != linked.ID || in.Outcomes[0].Details != linked.Details || !reflect.DeepEqual(in.Candidates[0], c) || in.Evidence[0].Text != evidence.Text {
		t.Fatal("lost linkage, report, evidence or uncertainty", in)
	}
	again, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil || !reflect.DeepEqual(in, again) {
		t.Fatal("nondeterministic selection", err)
	}
	w.Config.Runner.MaxInputBytes = exact - 1
	if _, err := w.input([]capture.Item{{Candidate: c}}); err == nil || !strings.Contains(err.Error(), "required linked outcome context cannot fit") {
		t.Fatal("required context silently truncated or misdiagnosed", err)
	}
}

func TestBoundedContextKeepsProtectedDuplicateAndSameDayLinks(t *testing.T) {
	w, _, c := fixture(t, "live")
	c.Refs = []string{"gh:pr:github.com/o/r#9"}
	sameDay := appendContextOutcome(t, w, c, 0, c.Refs, "another-turn")
	previous := c
	previous.OccurredAt = w.now().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	old := appendContextOutcome(t, w, previous, 1, c.Refs, "old-turn")
	humanCandidate := c
	human := appendContextOutcome(t, w, humanCandidate, 2, c.Refs, "human-amended")
	mutation := event.Event{Version: Version, ID: event.NewID(w.now()), PublicationKey: "synthetic-human-pin", Source: "human:cli", Type: "amend", TLDR: "Protected synthetic wording", RecordedAt: w.now().Format(time.RFC3339Nano), OccurredAt: c.OccurredAt, TimeBasis: "human", Targets: []event.Target{{ID: human.ID, Revision: 1}}}
	if err := store.Append(mutation); err != nil {
		t.Fatal(err)
	}
	// Exact provenance must survive even outside the recent window and without
	// a shared episode/ref, so replay cannot evade a persisted dismissal.
	duplicate := c
	duplicate.OccurredAt = w.now().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	e := event.Event{Version: Version, ID: event.NewID(w.now()), PublicationKey: "synthetic-duplicate", Source: "agent:pi", Type: "work", TLDR: "Previously handled report", RecordedAt: w.now().Format(time.RFC3339Nano), OccurredAt: duplicate.OccurredAt, TimeBasis: "report", Context: c.Context, Provenance: &event.Provenance{Candidates: []string{c.ID}, Episode: "old-unrelated"}}
	if err := store.Append(e); err != nil {
		t.Fatal(err)
	}
	mutation.ID = event.NewID(w.now())
	mutation.PublicationKey = "synthetic-dismissal"
	mutation.Type = "dismiss"
	mutation.TLDR = ""
	mutation.Reason = "synthetic dismissal"
	mutation.Targets = []event.Target{{ID: e.ID, Revision: 1}}
	if err := store.Append(mutation); err != nil {
		t.Fatal(err)
	}
	required, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	w.Config.Runner.MaxInputBytes = inputBytes(t, required)
	for i := 10; i < 90; i++ {
		appendContextOutcome(t, w, c, i, nil, fmt.Sprintf("other-%d", i))
	}
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Outcomes) != 4 || len(in.EditableTargets) != 1 || in.EditableTargets[0].ID != sameDay.ID {
		t.Fatalf("wrong context/targets: %+v", in)
	}
	byID := map[string]event.Entry{}
	for _, outcome := range in.Outcomes {
		byID[outcome.ID] = outcome
	}
	if !byID[human.ID].Pinned || !byID[e.ID].Dismissed || byID[old.ID].ID == "" {
		t.Fatal("missing protected/previous-day context", byID)
	}
	a := Action{Kind: "amend", Candidates: []string{c.ID}, Targets: in.EditableTargets, Type: "work", Text: "Updated bounded outcome", Reason: "same-day update"}
	if err := Validate(in, Output{Version: Version, Actions: []Action{a}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{human.ID, old.ID, e.ID} {
		a.Targets = []event.Target{{ID: id, Revision: byID[id].Revision}}
		if Validate(in, Output{Version: Version, Actions: []Action{a}}) == nil {
			t.Fatal("protected or previous-day edit admitted", id)
		}
	}
	a.Kind = "publish"
	a.Targets = nil
	if err := Validate(in, Output{Version: Version, Actions: []Action{a}}); err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatal("protected duplicate was republished", err)
	}
}

func TestRequiredContextOverflowIsNotIndividualCandidateOverflow(t *testing.T) {
	for _, count := range []int{3, 65} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			w, f, c := fixture(t, "live")
			for i := 0; i < count; i++ {
				appendContextOutcome(t, w, c, i, nil, c.Episode)
			}
			if count == 3 {
				w.Config.Runner.MaxInputBytes = 2000
			}
			other := c
			other.ID = ""
			other.Context.Task = "separate"
			other.Context.Repository.Path = "elsewhere/project"
			if _, err := w.Queue.Enqueue(other); err != nil {
				t.Fatal(err)
			}
			w.Config.Runner.CallsPerRun = 1
			res, err := w.Once(context.Background(), false)
			if err == nil || !strings.Contains(err.Error(), "required linked outcome context") || strings.Contains(err.Error(), "candidate "+c.ID+" cannot fit") {
				t.Fatal("wrong overflow classification", err)
			}
			if f.calls != 1 || res.Calls != 1 || res.Events != 1 {
				t.Fatal("context error blocked unrelated report", res, f.calls)
			}
			var budget Budget
			if err := durable.Read(filepath.Join(w.Queue.Root, "budget.json"), &budget); err != nil || budget.Calls != 1 {
				t.Fatal(budget, err)
			}
		})
	}
}

func TestMergedLinkedContextRetainsSurvivorAndProtection(t *testing.T) {
	w, _, c := fixture(t, "live")
	first := appendContextOutcome(t, w, c, 0, nil, "survivor")
	second := appendContextOutcome(t, w, c, 1, nil, c.Episode)
	merge := event.Event{Version: Version, ID: event.NewID(w.now()), PublicationKey: "synthetic-merge", Source: "human:cli", Type: "merge", TLDR: "Combined synthetic outcome", Details: "A later phase remains blocked.", RecordedAt: w.now().Format(time.RFC3339Nano), OccurredAt: c.OccurredAt, TimeBasis: "human", Targets: []event.Target{{ID: first.ID, Revision: 1}, {ID: second.ID, Revision: 1}}}
	if err := store.Append(merge); err != nil {
		t.Fatal(err)
	}
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	w.Config.Runner.MaxInputBytes = inputBytes(t, in)
	for i := 2; i < 72; i++ {
		appendContextOutcome(t, w, c, i, nil, fmt.Sprintf("optional-%d", i))
	}
	in, err = w.input([]capture.Item{{Candidate: c}})
	if err != nil || len(in.Outcomes) != 2 || len(in.EditableTargets) != 0 {
		t.Fatal(in, err)
	}
	byID := map[string]event.Entry{}
	for _, e := range in.Outcomes {
		byID[e.ID] = e
	}
	if byID[second.ID].MergedInto != first.ID || !byID[first.ID].Pinned || byID[first.ID].Details != merge.Details {
		t.Fatal("lost survivor, protection or uncertainty", byID)
	}
}

func TestOversizedReopenedHoldKeepsReceiptAndAllowsUnrelatedProgress(t *testing.T) {
	w, f, c := fixture(t, "live")
	f.fn = func(in Input) (Output, error) {
		return Output{Version: Version, Actions: []Action{{Kind: "hold", Candidates: []string{in.Candidates[0].ID}, Reason: "Synthetic unresolved conflict"}}}, nil
	}
	if _, err := w.Once(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	processed, err := w.Queue.List("processed")
	if err != nil || len(processed) != 1 {
		t.Fatal(processed, err)
	}
	before := processed[0].Receipt
	current := c
	current.ID = ""
	if _, err := w.Queue.Enqueue(current); err != nil {
		t.Fatal(err)
	}
	other := c
	other.ID, other.Context.Task = "", "independent"
	if _, err := w.Queue.Enqueue(other); err != nil {
		t.Fatal(err)
	}
	alone, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	w.Config.Runner.MaxInputBytes = inputBytes(t, alone) + 100
	w.Config.Runner.CallsPerRun = 1
	res, err := w.Once(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "current report batch cannot fit") || res.Calls != 1 || f.calls != 2 {
		t.Fatal(res, err, f.calls)
	}
	processed, err = w.Queue.List("processed")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range processed {
		if it.Candidate.ID == c.ID && !reflect.DeepEqual(it.Receipt, before) {
			t.Fatal("local overflow rewrote previously held receipt", it.Receipt, before)
		}
	}
}

func TestLargeBacklogSubmitsFittingBatchWithoutScanningTail(t *testing.T) {
	w, f, c := fixture(t, "live")
	for i := 0; i < 250; i++ {
		c.ID = ""
		c.CapturedAt = w.now().Add(-9 * time.Minute).Format(time.RFC3339Nano)
		c.Text = strings.Repeat("synthetic completed work. ", 100)
		if i == 249 {
			// This individually unfit tail item must wait behind the already
			// selected batch, not consume its preflight time or get failed now.
			c.Text = strings.Repeat("synthetic completed work. ", 250)
			c.CapturedAt = w.now().Add(-8 * time.Minute).Format(time.RFC3339Nano)
		}
		if _, err := w.Queue.Enqueue(c); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 80; i++ {
		appendContextOutcome(t, w, c, i, nil, fmt.Sprintf("optional-%d", i))
	}
	w.Config.Runner.MaxInputBytes = 4500
	w.Config.Runner.TimeoutSeconds = 1
	selected := 0
	f.fn = func(in Input) (Output, error) {
		selected = len(in.Candidates)
		out := Output{Version: Version}
		for _, candidate := range in.Candidates {
			out.Actions = append(out.Actions, Action{Kind: "skip", Candidates: []string{candidate.ID}, Reason: "synthetic duplicate"})
		}
		return out, nil
	}
	res, err := w.Once(context.Background(), false)
	if err != nil || res.Calls != 1 || res.Plans != 1 || f.calls != 1 || selected != 2 {
		t.Fatal("fitting batch starved behind oversized backlog", res, err, f.calls, selected)
	}
	pending, err := w.Queue.List("pending")
	if err != nil || len(pending) != 251-selected {
		t.Fatal("unselected backlog changed", len(pending), err)
	}
	for _, it := range pending {
		if it.Receipt.Attempts != 0 || it.Receipt.PlanID != "" {
			t.Fatal("preflight scanned/mutated deferred backlog", it.Receipt)
		}
	}
}

func TestPreflightBoundsIndividuallyUnfitLookahead(t *testing.T) {
	w, f, c := fixture(t, "live")
	for i := 0; i < 20; i++ {
		c.ID = ""
		c.CapturedAt = w.now().Add(-9 * time.Minute).Format(time.RFC3339Nano)
		c.Text = strings.Repeat("synthetic oversized report. ", 250)
		if _, err := w.Queue.Enqueue(c); err != nil {
			t.Fatal(err)
		}
	}
	w.Config.Runner.MaxInputBytes = 4500
	res, err := w.Once(context.Background(), false)
	if err == nil || res.Calls != 1 || res.Events != 1 || f.calls != 1 {
		t.Fatal("unfit lookahead blocked the fitting report", res, err, f.calls)
	}
	failed, err := w.Queue.List("error")
	if err != nil || len(failed) != w.Config.BatchSize-1 {
		t.Fatal("preflight did not bound examined reports", len(failed), err)
	}
	pending, err := w.Queue.List("pending")
	if err != nil || len(pending) != 21-w.Config.BatchSize {
		t.Fatal("deferred tail changed", len(pending), err)
	}
}

func TestBatchThatFitsSeparatelyDefersWithoutFailedReceipts(t *testing.T) {
	w, f, c := fixture(t, "live")
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	w.Config.Runner.MaxInputBytes = inputBytes(t, in)
	c.ID = ""
	second, err := w.Queue.Enqueue(c)
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Once(context.Background(), false)
	if err != nil || res.Calls != 1 || f.calls != 1 {
		t.Fatal(res, err)
	}
	items, err := w.Queue.List("pending")
	if err != nil || len(items) != 1 || items[0].Candidate.ID != second.ID || items[0].Receipt.Attempts != 0 || items[0].Receipt.PlanID != "" {
		t.Fatal("deferred batch mutated", items, err)
	}
}
