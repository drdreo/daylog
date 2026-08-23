package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/event"
)

// typeOrder pins the known types to a stable render order; unknown types
// follow in first-seen order rather than being dropped (§4.2).
var typeOrder = []string{event.TypeWork, event.TypeSidequest, event.TypeTodo, event.TypeNote}

var typeHeadings = map[string]string{
	event.TypeWork:       "Work",
	event.TypeSidequest:  "Side quests",
	event.TypeTodo:       "Todos filed today",
	event.TypeNote:       "Notes",
	event.TypeTransition: "Transitions",
}

// Markdown renders the folded day as the human view. It is a derived
// artifact, regenerable at any time, never the source of truth (§2).
func Markdown(d Day) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Daylog — %s\n", d.Date)

	groups := map[string][]Entry{}
	var order []string
	seen := map[string]bool{}
	for _, t := range typeOrder {
		order = append(order, t)
		seen[t] = true
	}
	for _, e := range d.Entries {
		if !seen[e.Type] {
			order = append(order, e.Type)
			seen[e.Type] = true
		}
		groups[e.Type] = append(groups[e.Type], e)
	}

	if len(d.Entries) == 0 {
		b.WriteString("\n_No entries._\n")
	}
	for _, t := range order {
		entries := groups[t]
		if len(entries) == 0 {
			continue
		}
		heading := typeHeadings[t]
		if heading == "" {
			heading = t
		}
		fmt.Fprintf(&b, "\n## %s\n\n", heading)
		for _, e := range entries {
			b.WriteString(entryLine(e))
		}
	}

	if len(d.PRs) > 0 || d.PRsFetchedAt != "" {
		fmt.Fprintf(&b, "\n## Open PRs%s\n\n", snapshotAge(d.PRsFetchedAt, d.GeneratedAt))
		if len(d.PRs) == 0 {
			b.WriteString("_None._\n")
		}
		for _, pr := range d.PRs {
			fmt.Fprintf(&b, "- **%s#%d** %s — %s\n", pr.Repo, pr.Number, pr.Title, prStatusLabel(&pr))
		}
	}

	if len(d.OpenTodos) > 0 {
		b.WriteString("\n## Open todos\n\n")
		for _, e := range d.OpenTodos {
			b.WriteString(entryLine(e))
		}
	}
	if len(d.AgentInbox) > 0 {
		b.WriteString("\n## Agent inbox (awaiting triage)\n\n")
		for _, e := range d.AgentInbox {
			b.WriteString(entryLine(e))
		}
	}
	return b.String()
}

func entryLine(e Entry) string {
	var b strings.Builder
	mark := " "
	if e.Done {
		mark = "x"
	}
	b.WriteString(fmt.Sprintf("- [%s] %s **%s** — %s", mark, clock(e.TS), e.Source, e.TLDR))
	if len(e.Refs) > 0 {
		b.WriteString(" (" + strings.Join(e.Refs, ", ") + ")")
	}
	if e.PR != nil {
		b.WriteString(fmt.Sprintf(" [%s]", prStatusLabel(e.PR)))
	}
	if e.OriginalType != "" {
		b.WriteString(fmt.Sprintf(" _(was %s)_", e.OriginalType))
	}
	if e.DoneNote != "" {
		b.WriteString(fmt.Sprintf(" _(closed: %s)_", e.DoneNote))
	}
	b.WriteString(fmt.Sprintf(" `%s`\n", event.ShortID(e.ID)))
	return b.String()
}

// snapshotAge annotates the Open PRs heading with the snapshot's honest age
// (§4.4): the fetch time normally, loudly marked stale once it is hours old —
// a poller that stopped running must not masquerade as current truth.
func snapshotAge(fetchedAt, generatedAt string) string {
	f, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return ""
	}
	g, err := time.Parse(time.RFC3339, generatedAt)
	if err == nil && g.Sub(f) > 2*time.Hour {
		return fmt.Sprintf(" (STALE — fetched %s)", f.Local().Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf(" (as of %s)", f.Local().Format("15:04"))
}

func clock(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "??:??"
	}
	return t.Format("15:04")
}
