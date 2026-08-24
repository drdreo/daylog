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
	if len(day.OpenTodos) != 0 || len(day.NeedsTriage) != 0 {
		t.Errorf("closed todo still listed open: todos=%v needs_triage=%v", day.OpenTodos, day.NeedsTriage)
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

func TestFoldTodosSpanDaysAndShareOneList(t *testing.T) {
	old := ev("2026-08-20", event.TypeTodo, "human:cli", "renew the cert")
	agent := ev("2026-08-20", event.TypeTodo, "agent:codex", "review the retry logic")
	today := ev("2026-08-23", event.TypeWork, "agent:claude", "shipped the parser")

	day := Fold([]event.Event{old, agent, today}, date("2026-08-23"), time.Now())

	if len(day.Entries) != 1 || day.Entries[0].TLDR != "shipped the parser" {
		t.Errorf("entries should only cover the requested day: %+v", day.Entries)
	}
	// One list to work from, regardless of who filed it.
	if len(day.OpenTodos) != 2 {
		t.Errorf("both todos should be open, got %d: %+v", len(day.OpenTodos), day.OpenTodos)
	}
	// needs_triage filters that list, it does not partition it.
	if len(day.NeedsTriage) != 1 || day.NeedsTriage[0].TLDR != "review the retry logic" {
		t.Errorf("only the untriaged agent todo needs a verdict: %+v", day.NeedsTriage)
	}
}

// The bug this whole change exists to kill: an open todo filed today used
// to appear in the work log AND the todo list, rendering twice in every
// widget. The work log records what happened; an open todo hasn't.
func TestFoldOpenTodoStaysOutOfTheWorkLog(t *testing.T) {
	todo := ev("2026-08-23", event.TypeTodo, "agent:claude", "plan the ad loop flow")
	work := ev("2026-08-23", event.TypeWork, "agent:claude", "shipped the parser")

	day := Fold([]event.Event{todo, work}, date("2026-08-23"), time.Now())

	if len(day.Entries) != 1 || day.Entries[0].TLDR != "shipped the parser" {
		t.Errorf("open todo must not appear in the work log: %+v", day.Entries)
	}
	if len(day.OpenTodos) != 1 {
		t.Errorf("open todo should still be listed once: %+v", day.OpenTodos)
	}

	// Closing it makes it a record of something that happened, so it earns
	// its place in the day's log.
	done := ev("2026-08-23", event.TypeDone, "human:cli", "planned it")
	done.Parent = &todo.ID
	day = Fold([]event.Event{todo, work, done}, date("2026-08-23"), time.Now())
	if len(day.Entries) != 2 {
		t.Errorf("closed todo belongs in the work log: %+v", day.Entries)
	}
	if len(day.OpenTodos) != 0 {
		t.Errorf("closed todo should leave the todo list: %+v", day.OpenTodos)
	}
}

func TestFoldTriageVerdicts(t *testing.T) {
	todo := ev("2026-08-22", event.TypeTodo, "agent:claude", "consider caching embeddings")

	accept := ev("2026-08-23", event.TypeTriage, "human:cli", "accepted")
	accept.Parent = &todo.ID
	accept.Meta = map[string]any{"verdict": event.VerdictAccepted}

	day := Fold([]event.Event{todo, accept}, date("2026-08-23"), time.Now())
	if len(day.OpenTodos) != 1 {
		t.Errorf("accepting keeps the todo open: %+v", day.OpenTodos)
	}
	if len(day.NeedsTriage) != 0 {
		t.Errorf("accepted todo should stop nagging: %+v", day.NeedsTriage)
	}
	if len(day.Entries) != 0 {
		t.Errorf("triage is bookkeeping, not a log entry: %+v", day.Entries)
	}

	// Declining hides it from every view — the ledger keeps it, the UI does not.
	decline := ev("2026-08-24", event.TypeTriage, "human:cli", "declined")
	decline.Parent = &todo.ID
	decline.Meta = map[string]any{"verdict": event.VerdictDeclined}

	day = Fold([]event.Event{todo, accept, decline}, date("2026-08-24"), time.Now())
	if len(day.OpenTodos) != 0 || len(day.NeedsTriage) != 0 || len(day.Entries) != 0 {
		t.Errorf("declined todo must not render: todos=%v triage=%v entries=%v",
			day.OpenTodos, day.NeedsTriage, day.Entries)
	}

	// Append-only means reversible: a later accept takes it back.
	reAccept := ev("2026-08-25", event.TypeTriage, "human:cli", "accepted after all")
	reAccept.Parent = &todo.ID
	reAccept.Meta = map[string]any{"verdict": event.VerdictAccepted}

	day = Fold([]event.Event{todo, accept, decline, reAccept}, date("2026-08-25"), time.Now())
	if len(day.OpenTodos) != 1 {
		t.Errorf("last verdict wins, so it should be open again: %+v", day.OpenTodos)
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
	reclassified, _, _ := Resolutions([]event.Event{note, rec, rev})
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
	if len(day.NeedsTriage) != 0 {
		t.Errorf("reclassified todo should leave the inbox: %+v", day.NeedsTriage)
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
