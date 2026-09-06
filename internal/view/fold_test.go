package view

import (
	"github.com/drdreo/daylog/internal/event"
	"strings"
	"testing"
	"time"
)

func TestOccurrenceAndCorrections(t *testing.T) {
	yesterday := "2026-09-05T23:59:00-07:00"
	today := "2026-09-06T01:00:00-07:00"
	a := event.Event{ID: "a", Type: event.TypeWork, TLDR: "before", Source: "agent:pi", OccurredAt: yesterday, RecordedAt: today}
	todo := event.Event{ID: "todo", Type: event.TypeTodo, TLDR: "task", Source: "human:cli", OccurredAt: yesterday}
	done := event.Event{Type: event.TypeDone, OccurredAt: today, Targets: []event.Target{{ID: "todo", Revision: 1}}}
	amend := event.Event{Type: event.TypeAmend, TLDR: "after", Source: "human:cli", OccurredAt: today, Targets: []event.Target{{ID: "a", Revision: 1}}}
	all := []event.Event{a, todo, done, amend}
	date, _ := time.Parse("2006-01-02", "2026-09-06")
	d := Fold(all, date, date)
	if len(d.Entries) != 1 || d.Entries[0].ID != "todo" || d.Entries[0].DisplayAt != today || len(d.OpenTodos) != 0 {
		t.Fatal(d)
	}
	d = Fold(all, date.AddDate(0, 0, -1), date)
	if len(d.Entries) != 1 || d.Entries[0].TLDR != "after" || !d.Entries[0].Pinned {
		t.Fatal(d)
	}
	if !strings.Contains(Markdown(d), "23:59") {
		t.Fatal(Markdown(d))
	}
}
func TestDismissRestoreMergeAndProposals(t *testing.T) {
	at := "2026-09-06T10:00:00Z"
	date, _ := time.Parse(time.RFC3339, at)
	a := event.Event{ID: "a", Type: event.TypeWork, Source: "agent:pi", OccurredAt: at}
	b := a
	b.ID = "b"
	todo := a
	todo.ID = "todo"
	todo.Type = event.TypeTodo
	all := []event.Event{a, b, todo, {Type: event.TypeMerge, TLDR: "merged", Targets: []event.Target{{ID: "a", Revision: 1}, {ID: "b", Revision: 1}}}, {Type: event.TypeDismiss, Targets: []event.Target{{ID: "a", Revision: 2}}}}
	d := Fold(all, date, date)
	if len(d.Entries) != 0 || len(d.OpenTodos) != 1 || len(d.NeedsTriage) != 1 {
		t.Fatal(d)
	}
	all = append(all, event.Event{Type: event.TypeRestore, Targets: []event.Target{{ID: "a", Revision: 3}}})
	d = Fold(all, date, date)
	if len(d.Entries) != 1 || d.Entries[0].TLDR != "merged" || len(d.Entries[0].Contributors) != 2 {
		t.Fatal(d)
	}
}
