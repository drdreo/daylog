package view

import (
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/event"
)

var seq int

func ev(day string, typ, source, tldr string) event.Event {
	seq++
	t, _ := time.Parse("2006-01-02", day)
	return event.Event{
		ID:     event.NewID(t.Add(time.Duration(seq) * time.Minute)),
		TS:     t.Add(time.Duration(seq) * time.Minute).Format(time.RFC3339),
		Host:   "test",
		Source: source,
		Type:   typ,
		TLDR:   tldr,
		Meta:   map[string]any{},
	}
}

func date(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestFoldReclassifyAndDone(t *testing.T) {
	work := ev("2026-08-23", event.TypeSidequest, "agent:claude", "cleaned up the flaky test")
	todo := ev("2026-08-23", event.TypeTodo, "agent:claude", "rotate the leaked token")

	rec := ev("2026-08-23", event.TypeReclassify, "human:cli", "promoted")
	rec.Parent = &work.ID
	rec.Meta = map[string]any{"to": "work"}

	done := ev("2026-08-23", event.TypeDone, "human:cli", "rotated it")
	done.Parent = &todo.ID

	day := Fold([]event.Event{work, todo, rec, done}, date("2026-08-23"), time.Now())

	if len(day.Entries) != 2 {
		t.Fatalf("want 2 entries (bookkeeping folded away), got %d", len(day.Entries))
	}
	if day.Entries[0].Type != "work" || day.Entries[0].OriginalType != "sidequest" {
		t.Errorf("reclassify not applied: type=%q original=%q", day.Entries[0].Type, day.Entries[0].OriginalType)
	}
	if !day.Entries[1].Done || day.Entries[1].DoneNote != "rotated it" {
		t.Errorf("done not applied: %+v", day.Entries[1])
	}
	if len(day.OpenTodos) != 0 || len(day.AgentInbox) != 0 {
		t.Errorf("closed todo still listed open: todos=%v inbox=%v", day.OpenTodos, day.AgentInbox)
	}
}

func TestFoldLastReclassifyWins(t *testing.T) {
	e := ev("2026-08-23", event.TypeNote, "human:cli", "spike on caching")
	r1 := ev("2026-08-23", event.TypeReclassify, "human:cli", "r1")
	r1.Parent = &e.ID
	r1.Meta = map[string]any{"to": "sidequest"}
	r2 := ev("2026-08-23", event.TypeReclassify, "human:cli", "r2")
	r2.Parent = &e.ID
	r2.Meta = map[string]any{"to": "work"}

	day := Fold([]event.Event{e, r1, r2}, date("2026-08-23"), time.Now())
	if day.Entries[0].Type != "work" {
		t.Errorf("last reclassify should win, got %q", day.Entries[0].Type)
	}
}

func TestFoldTodosSpanDaysAndSplitByNamespace(t *testing.T) {
	old := ev("2026-08-20", event.TypeTodo, "human:cli", "renew the cert")
	agent := ev("2026-08-20", event.TypeTodo, "agent:codex", "review the retry logic")
	today := ev("2026-08-23", event.TypeWork, "agent:claude", "shipped the parser")

	day := Fold([]event.Event{old, agent, today}, date("2026-08-23"), time.Now())

	if len(day.Entries) != 1 || day.Entries[0].TLDR != "shipped the parser" {
		t.Errorf("entries should only cover the requested day: %+v", day.Entries)
	}
	if len(day.OpenTodos) != 1 || day.OpenTodos[0].TLDR != "renew the cert" {
		t.Errorf("human todo from an earlier day should stay open: %+v", day.OpenTodos)
	}
	if len(day.AgentInbox) != 1 || day.AgentInbox[0].TLDR != "review the retry logic" {
		t.Errorf("agent todo belongs in the inbox: %+v", day.AgentInbox)
	}
}

func TestFoldToleratesUnknownTypes(t *testing.T) {
	e := ev("2026-08-23", "future-type", "poller:newthing", "something new happened")
	day := Fold([]event.Event{e}, date("2026-08-23"), time.Now())
	if len(day.Entries) != 1 || day.Entries[0].Type != "future-type" {
		t.Errorf("unknown types must render generically, got %+v", day.Entries)
	}
	// and markdown must not choke on them either
	if md := Markdown(day); md == "" {
		t.Error("markdown render produced nothing for unknown type")
	}
}

func TestNotePromotedToTodoIsTracked(t *testing.T) {
	note := ev("2026-08-22", event.TypeNote, "human:cli", "cache the embeddings")
	rec := ev("2026-08-23", event.TypeReclassify, "human:cli", "promoting")
	rec.Parent = &note.ID
	rec.Meta = map[string]any{"to": "todo"}

	day := Fold([]event.Event{note, rec}, date("2026-08-23"), time.Now())
	if len(day.OpenTodos) != 1 || day.OpenTodos[0].ID != note.ID {
		t.Fatalf("promoted note should be an open todo: %+v", day.OpenTodos)
	}

	// and the round trip: reverting to note untracks it again
	rev := ev("2026-08-23", event.TypeReclassify, "human:cli", "reverting")
	rev.Parent = &note.ID
	rev.Meta = map[string]any{"to": "note"}
	day = Fold([]event.Event{note, rec, rev}, date("2026-08-23"), time.Now())
	if len(day.OpenTodos) != 0 {
		t.Errorf("reverted note should leave open todos: %+v", day.OpenTodos)
	}

	// Resolutions (used by CLI target resolution) must agree
	reclassified, _ := Resolutions([]event.Event{note, rec, rev})
	if reclassified[note.ID] != "note" {
		t.Errorf("effective type after revert = %q, want note", reclassified[note.ID])
	}
}

func TestReclassifiedTodoLeavesQueue(t *testing.T) {
	todo := ev("2026-08-22", event.TypeTodo, "agent:claude", "consider caching embeddings")
	rec := ev("2026-08-23", event.TypeReclassify, "human:cli", "adopting")
	rec.Parent = &todo.ID
	rec.Meta = map[string]any{"to": "work", "triaged": true}

	day := Fold([]event.Event{todo, rec}, date("2026-08-23"), time.Now())
	if len(day.AgentInbox) != 0 {
		t.Errorf("reclassified todo should leave the inbox: %+v", day.AgentInbox)
	}
}

func TestMarkdownCheckboxesOnlyOnTodos(t *testing.T) {
	work := ev("2026-08-23", event.TypeWork, "agent:claude", "shipped the parser")
	trans := ev("2026-08-23", event.TypeTransition, "poller:gh", "PR merged: fix auth")
	openTodo := ev("2026-08-23", event.TypeTodo, "human:cli", "renew the cert")
	closedTodo := ev("2026-08-23", event.TypeTodo, "agent:codex", "rotate the token")
	done := ev("2026-08-23", event.TypeDone, "human:cli", "won't do")
	done.Parent = &closedTodo.ID

	md := Markdown(Fold([]event.Event{work, trans, openTodo, closedTodo, done}, date("2026-08-23"), time.Now()))

	for _, want := range []string{"- [ ] ", "- [x] "} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.Contains(line, "shipped the parser") || strings.Contains(line, "PR merged") {
			if strings.Contains(line, "[ ]") || strings.Contains(line, "[x]") {
				t.Errorf("non-todo line has a checkbox: %s", line)
			}
		}
		if strings.Contains(line, "renew the cert") && !strings.Contains(line, "- [ ]") {
			t.Errorf("open todo missing empty checkbox: %s", line)
		}
		if strings.Contains(line, "rotate the token") && !strings.Contains(line, "- [x]") {
			t.Errorf("closed todo missing checked box: %s", line)
		}
	}
}
