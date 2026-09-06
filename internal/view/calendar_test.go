package view

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/event"
)

func TestCalendarMatchesFoldedJournal(t *testing.T) {
	entry := func(id, kind, at string) event.Event {
		return event.Event{ID: id, Type: kind, Source: "human:cli", OccurredAt: at, RecordedAt: "2026-10-01T12:00:00Z"}
	}
	correction := func(kind, id string, revision int) event.Event {
		return event.Event{Type: kind, Source: "human:cli", Targets: []event.Target{{ID: id, Revision: revision}}, OccurredAt: "2026-09-07T00:01:00+14:00"}
	}
	all := []event.Event{
		entry("late", event.TypeWork, "2026-08-31T23:59:00-07:00"),
		entry("early", event.TypeNote, "2026-09-01T00:01:00+14:00"),
		entry("open", event.TypeTodo, "2026-09-02T12:00:00Z"),
		entry("closed", event.TypeTodo, "2026-08-15T12:00:00Z"),
		correction(event.TypeDone, "closed", 1),
		entry("hidden", event.TypeWork, "2026-09-03T12:00:00Z"),
		correction(event.TypeDismiss, "hidden", 1),
		entry("restored", event.TypeNote, "2026-09-04T12:00:00Z"),
		correction(event.TypeDismiss, "restored", 1),
		correction(event.TypeRestore, "restored", 2),
		entry("primary", event.TypeWork, "2026-09-05T12:00:00Z"),
		entry("duplicate", event.TypeWork, "2026-09-05T13:00:00Z"),
		{Type: event.TypeMerge, Targets: []event.Target{{ID: "primary", Revision: 1}, {ID: "duplicate", Revision: 1}}},
		correction(event.TypeAmend, "primary", 2),
		entry("declined", event.TypeTodo, "2026-09-06T12:00:00Z"),
		{Type: event.TypeTriage, Verdict: event.VerdictDeclined, Targets: []event.Target{{ID: "declined", Revision: 1}}},
		entry("also", event.TypeNote, "2026-09-07T12:00:00Z"),
		entry("next", event.TypeNote, "2026-10-01T12:00:00Z"),
	}
	month, _ := time.Parse("2006-01", "2026-09")
	got := CalendarMonth(all, month)
	want := []DayCount{{"2026-09-01", 1}, {"2026-09-04", 1}, {"2026-09-05", 1}, {"2026-09-07", 2}}
	if got.Version != event.Version || got.Month != "2026-09" || !reflect.DeepEqual(got.Days, want) {
		t.Fatalf("got %+v; want %+v", got, want)
	}
	counts := map[string]int{}
	for _, day := range got.Days {
		counts[day.Date] = day.Count
	}
	for day := month; day.Month() == month.Month(); day = day.AddDate(0, 0, 1) {
		folded := Fold(all, day, month)
		if counts[folded.Date] != len(folded.Entries) {
			t.Fatalf("calendar disagrees with Fold for %s", folded.Date)
		}
	}
	previous := CalendarMonth(all, month.AddDate(0, -1, 0))
	if !reflect.DeepEqual(previous.Days, []DayCount{{"2026-08-31", 1}}) {
		t.Fatal(previous)
	}
}

func TestCalendarEmptyMonthUsesArray(t *testing.T) {
	data, err := json.Marshal(CalendarMonth(nil, time.Now()))
	if err != nil || !strings.Contains(string(data), `"days":[]`) {
		t.Fatal(string(data), err)
	}
}
