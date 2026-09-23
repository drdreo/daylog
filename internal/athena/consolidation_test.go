package athena

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

func TestSamePRReportsAcrossTurnsAmendOneOutcome(t *testing.T) {
	testSameGitHubRefReportsAcrossTurns(t, "gh:pr:github.com/o/r#142")
}

func TestSameIssueReportsAcrossTurnsAmendOneOutcome(t *testing.T) {
	testSameGitHubRefReportsAcrossTurns(t, "gh:issue:github.com/o/r#142")
}

func testSameGitHubRefReportsAcrossTurns(t *testing.T, ref string) {
	t.Helper()
	w, f, c := fixture(t, "live")
	// The initial report has no ref; following reports use an explicit stable ref.
	f.fn = func(in Input) (Output, error) {
		return Output{Version: Version, Actions: []Action{{Kind: "publish", Candidates: []string{in.Candidates[0].ID}, Type: "work", Text: "Improved image-edit keyboard behavior", Reason: "user-visible behavior"}}}, nil
	}
	// Use the original episode for the first update to attach its supplied ref.
	if _, err := w.Once(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= 3; step++ {
		c.ID = ""
		c.Text = fmt.Sprintf("Keyboard behavior update %d; later processing-focus change remains browser-untested.", step)
		c.Refs = []string{ref}
		if step > 1 {
			c.Context.Task = fmt.Sprintf("separate-turn-%d", step)
		}
		if _, err := w.Queue.Enqueue(c); err != nil {
			t.Fatal(err)
		}
		f.fn = func(in Input) (Output, error) {
			if len(in.EditableTargets) != 1 {
				t.Fatalf("missing same-day target: %+v", in.EditableTargets)
			}
			return Output{Version: Version, Actions: []Action{{Kind: "amend", Candidates: []string{in.Candidates[0].ID}, Targets: in.EditableTargets, Type: "work", Text: "Improved image-edit keyboard behavior", Details: "Earlier Escape behavior passed browser checks; the later processing-focus change remains untested.", Refs: []string{ref}, Reason: "same outcome, new scope or verification"}}}, nil
		}
		if res, err := w.Once(context.Background(), false); err != nil || res.Events != 1 {
			t.Fatal(res, err)
		}
	}
	all, err := store.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	effective := event.Effective(all)
	if len(all) != 4 || len(effective) != 1 {
		t.Fatalf("want 4 append-only events, 1 outcome: %d/%d", len(all), len(effective))
	}
	for _, e := range effective {
		if e.Revision != 4 || len(e.Provenance.Candidates) != 4 || e.DisplayAt != c.OccurredAt || len(e.Refs) != 1 || e.Refs[0] != ref {
			t.Fatal(e)
		}
	}
}

func TestEditableTargetsMatchValidation(t *testing.T) {
	_, _, c := fixture(t, "live")
	c.Refs = []string{"gh:pr:github.com/o/r#142"}
	base := event.Entry{Event: event.Event{ID: "existing", Source: "agent:pi", Type: "work", Context: c.Context, Refs: c.Refs, Provenance: &event.Provenance{Episode: "another-turn"}}, Revision: 1, DisplayAt: c.OccurredAt}
	cases := []struct {
		name    string
		change  func(*event.Entry)
		allowed bool
	}{
		{"shared PR", func(*event.Entry) {}, true},
		{"same episode without ref", func(e *event.Entry) { e.Refs = nil; e.Provenance = &event.Provenance{Episode: c.Episode} }, true},
		{"same repo and session alone", func(e *event.Entry) { e.Refs = nil }, false},
		{"different PR", func(e *event.Entry) { e.Refs = []string{"gh:pr:github.com/o/r#143"} }, false},
		{"different repository", func(e *event.Entry) { e.Context.Repository.Path = "other/repo" }, false},
		{"different host", func(e *event.Entry) { e.Context.Repository.Host = "github.example.com" }, false},
		{"previous day", func(e *event.Entry) { e.DisplayAt = "2026-09-05T12:00:00Z" }, false},
		{"human", func(e *event.Entry) { e.Source = "human:cli" }, false},
		{"pinned", func(e *event.Entry) { e.Pinned = true }, false},
		{"dismissed", func(e *event.Entry) { e.Dismissed = true }, false},
		{"merged", func(e *event.Entry) { e.MergedInto = "other" }, false},
		{"todo", func(e *event.Entry) { e.Type = "todo" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.change(&e)
			in := Input{Version: Version, Candidates: []capture.Candidate{c}, Outcomes: []event.Entry{e}}
			in.EditableTargets = editableTargets(in)
			if (len(in.EditableTargets) == 1) != tc.allowed {
				t.Fatal(in.EditableTargets)
			}
			a := Action{Kind: "amend", Candidates: []string{c.ID}, Targets: []event.Target{{ID: e.ID, Revision: e.Revision}}, Type: "work", Text: "Updated outcome", Reason: "same-day update"}
			if err := Validate(in, Output{Version: Version, Actions: []Action{a}}); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
			a.Targets[0].Revision++
			if Validate(in, Output{Version: Version, Actions: []Action{a}}) == nil {
				t.Fatal("stale revision accepted")
			}
		})
	}
	for _, ref := range []string{"linear:ABC-7", "gh:issue:github.com/o/r#142"} {
		c.Refs = []string{ref}
		base.Refs = c.Refs
		if !linkedOutcome(c, base) {
			t.Fatal("exact issue ref should also link", ref)
		}
	}
	for _, ref := range []string{"gh:pr:github.com/o/r#142", "gh:issue:github.com/o/r#143"} {
		base.Refs = []string{ref}
		if linkedOutcome(c, base) {
			t.Fatal("issue linked to a PR or different issue", ref)
		}
	}
}

func TestSameDaySharedRefAllowsMergeButNotCandidateEpisodeMixing(t *testing.T) {
	_, _, c := fixture(t, "live")
	c.Refs = []string{"linear:ABC-7"}
	in := Input{Version: Version, Candidates: []capture.Candidate{c}}
	for _, id := range []string{"first", "second"} {
		in.Outcomes = append(in.Outcomes, event.Entry{Event: event.Event{ID: id, Source: "agent:pi", Type: "work", Context: c.Context, Refs: c.Refs, Provenance: &event.Provenance{Episode: id}}, Revision: 1, DisplayAt: c.OccurredAt})
	}
	in.EditableTargets = editableTargets(in)
	a := Action{Kind: "merge", Candidates: []string{c.ID}, Targets: in.EditableTargets, Type: "work", Text: "Completed feature-flag coverage", Reason: "one outcome"}
	if err := Validate(in, Output{Version: Version, Actions: []Action{a}}); err != nil {
		t.Fatal(err)
	}
	other := c
	other.ID, other.Episode = "other", "unrelated"
	in.Candidates = append(in.Candidates, other)
	a.Candidates = append(a.Candidates, other.ID)
	if Validate(in, Output{Version: Version, Actions: []Action{a}}) == nil {
		t.Fatal("shared ref silently combined unrelated candidate episodes")
	}
	in.Candidates[1].Episode = c.Episode
	in.Candidates[1].OccurredAt = "2026-09-07T12:00:00Z"
	if Validate(in, Output{Version: Version, Actions: []Action{a}}) == nil {
		t.Fatal("one action combined multiple occurrence days")
	}
}

func TestWorkerSeparatesOccurrenceDaysWithinOneEpisode(t *testing.T) {
	w, f, c := fixture(t, "live")
	c.ID = ""
	later := w.now().Add(24 * time.Hour)
	c.OccurredAt, c.CapturedAt = later.Add(-10*time.Minute).Format(time.RFC3339), later.Add(-10*time.Minute).Format(time.RFC3339)
	if _, err := w.Queue.Enqueue(c); err != nil {
		t.Fatal(err)
	}
	w.Now = func() time.Time { return later }
	f.fn = func(in Input) (Output, error) {
		if len(in.Candidates) != 1 {
			t.Fatal("mixed journal days", in.Candidates)
		}
		return Output{Version: Version, Actions: []Action{{Kind: "publish", Candidates: []string{in.Candidates[0].ID}, Type: "work", Text: "A distinct dated milestone", Reason: "new day"}}}, nil
	}
	if res, err := w.Once(context.Background(), false); err != nil || res.Events != 2 || f.calls != 2 {
		t.Fatal(res, err, f.calls)
	}
	all, err := store.ReadAll()
	if err != nil || len(all) != 2 || event.SameDay(all[0].OccurredAt, all[1].OccurredAt) {
		t.Fatal(all, err)
	}
}
