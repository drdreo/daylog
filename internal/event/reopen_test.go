package event

import (
	"testing"
	"time"
)

func TestReopenTodoLifecycle(t *testing.T) {
	filed := "2026-09-09T10:00:00+02:00"
	base := Event{ID: NewID(time.Now()), Type: TypeTodo, TLDR: "Task", Source: "human:widget", OccurredAt: filed}
	done := Event{Type: TypeDone, Source: "human:widget", OccurredAt: "2026-09-10T12:00:00+02:00", TLDR: "Finished", Targets: []Target{{base.ID, 1}}}
	reopen := Event{Version: Version, ID: NewID(time.Now()), PublicationKey: "test/reopen", RecordedAt: "2026-09-11T12:00:00+02:00", OccurredAt: "2026-09-11T12:00:00+02:00", TimeBasis: "human", Type: TypeReopen, Source: "human:widget", Targets: []Target{{base.ID, 2}}}
	if err := reopen.Validate(); err != nil {
		t.Fatal(err)
	}
	history := []Event{base, done, reopen}
	current := Effective(history[:2])
	if err := CheckTargets(reopen, current); err != nil {
		t.Fatal(err)
	}
	current = Effective([]Event{base, done, reopen})
	got := current[base.ID]
	if got.Done || got.DoneNote != "" || got.DisplayAt != filed || got.FiledAt != filed || got.Revision != 3 {
		t.Fatalf("reopened = %+v", got)
	}
	if CheckTargets(reopen, current) == nil {
		t.Fatal("stale reopen allowed")
	}
	reopen.Targets[0].Revision = 3
	if CheckTargets(reopen, current) == nil {
		t.Fatal("open todo reopened")
	}
	done.Targets[0].Revision = 3
	if err := CheckTargets(done, current); err != nil {
		t.Fatal(err)
	}
	done.OccurredAt = "2026-09-12T12:00:00+02:00"
	got = Effective(append(history, done))[base.ID]
	if !got.Done || got.DisplayAt != done.OccurredAt || got.FiledAt != filed {
		t.Fatalf("recompleted = %+v", got)
	}
	reopen.Source = "agent:pi"
	if reopen.Validate() == nil {
		t.Fatal("agent reopened todo")
	}
	reopen.Source = "human:widget"
	current[base.ID] = Entry{Event: Event{Type: TypeNote}, Revision: 3, Done: true}
	if CheckTargets(reopen, current) == nil {
		t.Fatal("non-todo reopened")
	}
}
