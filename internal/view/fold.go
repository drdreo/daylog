// Package view derives read-time state by folding over events (§2: events
// are the source of truth; everything else is derived). Its output is the
// consumer contract exposed as `daylog today --json`.
package view

import (
	"sort"
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/snapshot"
)

// Entry is an event with fold results applied: Type is the effective type
// after reclassification and Done reflects a closing `done` event. External
// live state is kept out of entries; it belongs in the separate snapshot
// collections on Day.
//
// TS and DoneTS are both kept: a closed todo has two moments worth showing —
// when it was taken on and when it was finished — and collapsing them would
// lose the one thing a completed todo actually tells you.
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
	DoneTS       string         `json:"done_ts,omitempty"`
	DoneNote     string         `json:"done_note,omitempty"`
	Verdict      string         `json:"verdict,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

// Day is the folded view object emitted by `daylog today --json`.
//
// entries is the day's work log: what actually happened. An open todo is
// an obligation, not an event, so it stays out of entries and lives in
// open_todos — otherwise the same line renders twice in every widget.
//
// open_todos spans all days (an obligation doesn't expire at midnight) and
// holds every open todo, agent- and human-filed alike: one list to work
// from. needs_triage is a *filter over that same list*, not a disjoint
// bucket — the agent-filed todos still awaiting a verdict (§5.2). Render
// open_todos; use needs_triage to mark rows and drive the attention badge.
//
// prs is every open PR from the GitHub snapshot, honest about its age via
// prs_fetched_at (empty when the poller has never run).
type Day struct {
	Date         string        `json:"date"`
	GeneratedAt  string        `json:"generated_at"`
	Entries      []Entry       `json:"entries"`
	OpenTodos    []Entry       `json:"open_todos"`
	NeedsTriage  []Entry       `json:"needs_triage"`
	PRs          []snapshot.PR `json:"prs"`
	PRsFetchedAt string        `json:"prs_fetched_at,omitempty"`
}

// Fold computes the Day view for `date` from the full event history.
// Resolution is deterministic: events are ULID-sorted, so for competing
// reclassify events the last ULID wins (§8), with all opinions preserved.
func Fold(all []event.Event, date time.Time, now time.Time) Day {
	reclassified, doneBy, verdicts := Resolutions(all)

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
			// Only a usable closing time is published. store.readFile
			// tolerates any line carrying an id (§10), so a done event can
			// arrive with a missing or unparseable ts; leaving it here would
			// strand the todo in no day at all — out of entries because its
			// day matches nothing, and out of open_todos because it is
			// closed. Absent done_ts means every consumer falls back to the
			// filing time, which is what they did before closing times
			// existed.
			if tsDay(d.TS) != "" {
				en.DoneTS = d.TS
			}
		}
		en.Verdict = verdicts[e.ID]
		return en
	}

	dayStr := date.Format("2006-01-02")
	day := Day{
		Date:        dayStr,
		GeneratedAt: now.Format(time.RFC3339),
		Entries:     []Entry{},
		OpenTodos:   []Entry{},
		NeedsTriage: []Entry{},
		PRs:         []snapshot.PR{},
	}
	for _, e := range all {
		// done, reclassify and triage are bookkeeping folded into their
		// targets; unknown types render generically as entries (§4.2).
		switch e.Type {
		case event.TypeDone, event.TypeReclassify, event.TypeTriage:
			continue
		}
		en := toEntry(e)

		// A declined proposal leaves the ledger intact but every view
		// behind: it was never yours to carry.
		if en.Verdict == event.VerdictDeclined {
			continue
		}

		isOpenTodo := en.Type == event.TypeTodo && !en.Done
		// A closed todo is filed under the day it was *closed*, not the day
		// it was filed: until then it was an obligation carried forward, and
		// it only became a record of something that happened when it was
		// finished. Anything else already happened on the day it was logged.
		entryDay := eventDay(e)
		if en.Type == event.TypeTodo && en.DoneTS != "" {
			entryDay = tsDay(en.DoneTS)
		}
		// The work log records what happened; an open todo hasn't.
		if !isOpenTodo && entryDay == dayStr {
			day.Entries = append(day.Entries, en)
		}
		if isOpenTodo {
			day.OpenTodos = append(day.OpenTodos, en)
			if strings.HasPrefix(en.Source, "agent:") && en.Verdict == "" {
				day.NeedsTriage = append(day.NeedsTriage, en)
			}
		}
	}
	// Closed todos enter the log at their closing time, so ULID order (which
	// is filing order) no longer matches the day's actual sequence.
	sort.SliceStable(day.Entries, func(i, j int) bool {
		return logTime(day.Entries[i]).Before(logTime(day.Entries[j]))
	})
	return day
}

// logTime is when an entry took its place in the day's log: the closing time
// for a closed todo, the event's own time for everything else.
func logTime(e Entry) time.Time {
	ts := e.TS
	if e.Type == event.TypeTodo && e.DoneTS != "" {
		ts = e.DoneTS
	}
	t, _ := time.Parse(time.RFC3339, ts)
	return t
}

// Resolutions folds the correction events into lookup maps: the effective
// type per reclassified entry, the closing done event per closed entry, and
// the standing triage verdict per proposed entry. Input is ULID-sorted, so
// for competing corrections the last one wins (§8) — which is also what
// lets a decline be reversed by a later accept. Shared by Fold and by CLI
// target resolution so both agree on an entry's current state.
func Resolutions(all []event.Event) (reclassified map[string]string, doneBy map[string]event.Event, verdicts map[string]string) {
	reclassified = map[string]string{}
	doneBy = map[string]event.Event{}
	verdicts = map[string]string{}
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
		case event.TypeTriage:
			if v, ok := e.Meta["verdict"].(string); ok && event.ValidateVerdict(v) == nil {
				verdicts[*e.Parent] = v
			}
		}
	}
	return reclassified, doneBy, verdicts
}

// eventDay extracts the calendar date of an event in its own recorded
// timezone offset, matching the file it was written to.
func eventDay(e event.Event) string { return tsDay(e.TS) }

func tsDay(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}
