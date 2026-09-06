// Package view exposes the versioned consumer contract; queue data never leaks into it.
package view

import (
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/snapshot"
	"sort"
	"strings"
	"time"
)

type Entry = event.Entry
type Day struct {
	Version      int           `json:"version"`
	Date         string        `json:"date"`
	GeneratedAt  string        `json:"generated_at"`
	Entries      []Entry       `json:"entries"`
	OpenTodos    []Entry       `json:"open_todos"`
	NeedsTriage  []Entry       `json:"needs_triage"`
	PRs          []snapshot.PR `json:"prs"`
	PRsFetchedAt string        `json:"prs_fetched_at,omitempty"`
}

func Fold(all []event.Event, date, now time.Time) Day {
	d := Day{Version: event.Version, Date: date.Format("2006-01-02"), GeneratedAt: now.Format(time.RFC3339Nano), Entries: []Entry{}, OpenTodos: []Entry{}, NeedsTriage: []Entry{}, PRs: []snapshot.PR{}}
	for _, e := range event.Effective(all) {
		if !visibleEntry(e) {
			continue
		}
		if e.Refs == nil {
			e.Refs = []string{}
		}
		if e.Type == event.TypeTodo && !e.Done {
			d.OpenTodos = append(d.OpenTodos, e)
			if !event.Human(e.Source) && e.Verdict == "" {
				d.NeedsTriage = append(d.NeedsTriage, e)
			}
		} else if strings.HasPrefix(e.DisplayAt, d.Date) {
			d.Entries = append(d.Entries, e)
		}
	}
	for _, entries := range [][]Entry{d.Entries, d.OpenTodos, d.NeedsTriage} {
		sort.Slice(entries, func(i, j int) bool {
			a, _ := time.Parse(time.RFC3339Nano, entries[i].DisplayAt)
			b, _ := time.Parse(time.RFC3339Nano, entries[j].DisplayAt)
			if a.Equal(b) {
				return entries[i].ID < entries[j].ID
			}
			return a.Before(b)
		})
	}
	return d
}
