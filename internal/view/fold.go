// Package view derives read-time state by folding over events (§2: events
// are the source of truth; everything else is derived). Its output is the
// consumer contract exposed as `daylog today --json`.
package view

import (
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/event"
)

// Entry is an event with fold results applied: Type is the effective type
// after reclassification, Done reflects a closing `done` event.
type Entry struct {
	ID           string         `json:"id"`
	TS           string         `json:"ts"`
	Host         string         `json:"host"`
	Source       string         `json:"source"`
	Type         string         `json:"type"`
	OriginalType string         `json:"original_type,omitempty"`
	TLDR         string         `json:"tldr"`
	Refs         []string       `json:"refs"`
	Ctx          event.Ctx      `json:"ctx"`
	Done         bool           `json:"done"`
	DoneNote     string         `json:"done_note,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

// Day is the folded view object emitted by `daylog today --json`.
// open_todos and agent_inbox span all days — an obligation doesn't expire
// at midnight; agent_inbox is the untriaged review queue (§5.2).
type Day struct {
	Date        string  `json:"date"`
	GeneratedAt string  `json:"generated_at"`
	Entries     []Entry `json:"entries"`
	OpenTodos   []Entry `json:"open_todos"`
	AgentInbox  []Entry `json:"agent_inbox"`
}

// Fold computes the Day view for `date` from the full event history.
// Resolution is deterministic: events are ULID-sorted, so for competing
// reclassify events the last ULID wins (§8), with all opinions preserved.
func Fold(all []event.Event, date time.Time, now time.Time) Day {
	reclassified := map[string]string{} // target id → effective type
	doneBy := map[string]event.Event{}  // target id → closing done event
	for _, e := range all {
		if e.Parent == nil {
			continue
		}
		switch e.Type {
		case event.TypeReclassify:
			if to, ok := e.Meta["to"].(string); ok && to != "" {
				reclassified[*e.Parent] = to
			}
		case event.TypeDone:
			doneBy[*e.Parent] = e
		}
	}

	toEntry := func(e event.Event) Entry {
		en := Entry{
			ID: e.ID, TS: e.TS, Host: e.Host, Source: e.Source,
			Type: e.Type, TLDR: e.TLDR, Refs: e.Refs, Ctx: e.Ctx, Meta: e.Meta,
		}
		if en.Refs == nil {
			en.Refs = []string{}
		}
		if len(en.Meta) == 0 {
			en.Meta = nil
		}
		if to, ok := reclassified[e.ID]; ok && to != e.Type {
			en.OriginalType = e.Type
			en.Type = to
		}
		if d, ok := doneBy[e.ID]; ok {
			en.Done = true
			en.DoneNote = d.TLDR
		}
		return en
	}

	dayStr := date.Format("2006-01-02")
	day := Day{
		Date:        dayStr,
		GeneratedAt: now.Format(time.RFC3339),
		Entries:     []Entry{},
		OpenTodos:   []Entry{},
		AgentInbox:  []Entry{},
	}
	for _, e := range all {
		// done and reclassify are bookkeeping folded into their targets;
		// unknown types render generically as entries (§4.2).
		isBookkeeping := e.Type == event.TypeDone || e.Type == event.TypeReclassify
		en := toEntry(e)

		if !isBookkeeping && eventDay(e) == dayStr {
			day.Entries = append(day.Entries, en)
		}
		if en.Type == event.TypeTodo && !en.Done {
			if strings.HasPrefix(en.Source, "agent:") {
				day.AgentInbox = append(day.AgentInbox, en)
			} else {
				day.OpenTodos = append(day.OpenTodos, en)
			}
		}
	}
	return day
}

// eventDay extracts the calendar date of an event in its own recorded
// timezone offset, matching the file it was written to.
func eventDay(e event.Event) string {
	t, err := time.Parse(time.RFC3339, e.TS)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}
