package view

import (
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/snapshot"
)

func testSnap() *snapshot.GHPRs {
	return &snapshot.GHPRs{
		FetchedAt: "2026-08-23T12:00:00Z",
		PRs: map[string]snapshot.PR{
			"gh:pr:github.com/o/r#7": {Ref: "gh:pr:github.com/o/r#7", Repo: "o/r", Number: 7, Title: "Fix races",
				State: "open", Checks: "failing", Review: "changes_requested"},
			"gh:pr:github.com/o/r#3": {Ref: "gh:pr:github.com/o/r#3", Repo: "o/r", Number: 3, Title: "Old one",
				State: "merged", Checks: "passing", Review: "approved"},
			"gh:pr:github.com/a/a#1": {Ref: "gh:pr:github.com/a/a#1", Repo: "a/a", Number: 1, Title: "Other repo",
				State: "open", Checks: "none", Review: "none", Draft: true},
		},
	}
}

func TestJoinGHLeavesNarrativeEntriesAlone(t *testing.T) {
	d := Day{
		Entries: []Entry{
			{Event: event.Event{ID: "A", Refs: []string{"linear:ABC-1", "gh:pr:github.com/o/r#7"}}},
			{Event: event.Event{ID: "B", Refs: []string{"gh:pr:github.com/o/r#999"}}}, // not in snapshot
			{Event: event.Event{ID: "C", Refs: []string{}}},
			{Event: event.Event{ID: "D", Refs: []string{"gh:issue:github.com/o/r#7"}}},
		},
		OpenTodos:   []Entry{{Event: event.Event{ID: "T", Refs: []string{"gh:pr:github.com/o/r#3"}}}},
		NeedsTriage: []Entry{},
	}
	JoinGH(&d, testSnap())

	if got := strings.Join(d.Entries[0].Refs, ","); got != "linear:ABC-1,gh:pr:github.com/o/r#7" {
		t.Fatalf("entry refs changed: %q", got)
	}
	if got := strings.Join(d.OpenTodos[0].Refs, ","); got != "gh:pr:github.com/o/r#3" {
		t.Fatalf("todo refs changed: %q", got)
	}
	if got := strings.Join(d.Entries[3].Refs, ","); got != "gh:issue:github.com/o/r#7" || len(d.PRs) != 2 {
		t.Fatalf("issue ref affected PR snapshot: ref=%q prs=%+v", got, d.PRs)
	}
	if d.PRsFetchedAt != "2026-08-23T12:00:00Z" {
		t.Errorf("prs_fetched_at = %q", d.PRsFetchedAt)
	}
}

func TestJoinGHListsOnlyOpenPRsSorted(t *testing.T) {
	d := Day{}
	JoinGH(&d, testSnap())
	if len(d.PRs) != 2 {
		t.Fatalf("prs = %d, want 2 (merged one excluded)", len(d.PRs))
	}
	if d.PRs[0].Ref != "gh:pr:github.com/a/a#1" || d.PRs[1].Ref != "gh:pr:github.com/o/r#7" {
		t.Fatalf("order = %s, %s", d.PRs[0].Ref, d.PRs[1].Ref)
	}
}

func TestJoinGHNilSnapshotIsNoop(t *testing.T) {
	d := Day{Entries: []Entry{{Event: event.Event{ID: "A", Refs: []string{"gh:pr:github.com/o/r#7"}}}}}
	JoinGH(&d, nil)
	if len(d.PRs) != 0 || d.PRsFetchedAt != "" {
		t.Fatal("nil snapshot must leave the day untouched")
	}
}

func TestPRStatusLabel(t *testing.T) {
	cases := []struct {
		pr   snapshot.PR
		want string
	}{
		{snapshot.PR{State: "merged"}, "merged"},
		{snapshot.PR{State: "closed"}, "closed"},
		{snapshot.PR{State: "open", Checks: "none", Review: "none"}, "open"},
		{snapshot.PR{State: "open", Checks: "failing", Review: "changes_requested"}, "checks failing · changes requested"},
		{snapshot.PR{State: "open", Draft: true, Checks: "pending", Review: "none"}, "draft · checks pending"},
		{snapshot.PR{State: "open", Checks: "passing", Review: "approved"}, "checks passing · approved"},
	}
	for i, c := range cases {
		if got := prStatusLabel(&c.pr); got != c.want {
			t.Errorf("case %d: label = %q, want %q", i, got, c.want)
		}
	}
}

func TestMarkdownKeepsPRStatusInItsOwnSection(t *testing.T) {
	d := Day{
		Date:        "2026-08-23",
		GeneratedAt: "2026-08-23T12:30:00Z",
		Entries: []Entry{{DisplayAt: "2026-08-23T09:00:00Z", Event: event.Event{
			ID:     "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			Source: "agent:claude", Type: "work", TLDR: "Fixed the race",
			Refs: []string{"gh:pr:github.com/o/r#7"},
		}}},
	}
	JoinGH(&d, testSnap())
	md := Markdown(d)
	if !strings.Contains(md, "## Open PRs (as of ") {
		t.Errorf("missing Open PRs section:\n%s", md)
	}
	if !strings.Contains(md, "**o/r#7** Fix races — checks failing · changes requested") {
		t.Errorf("missing PR line:\n%s", md)
	}
	if !strings.Contains(md, "Fixed the race (gh:pr:github.com/o/r#7)") {
		t.Errorf("missing work entry:\n%s", md)
	}
	if strings.Contains(md, "Fixed the race (gh:pr:github.com/o/r#7) [") {
		t.Errorf("PR status leaked into work entry:\n%s", md)
	}
	if strings.Contains(md, "o/r#3") {
		t.Errorf("merged PR leaked into Open PRs:\n%s", md)
	}
}

func TestMarkdownMarksStaleSnapshot(t *testing.T) {
	d := Day{
		Date:         "2026-08-23",
		GeneratedAt:  "2026-08-23T18:00:00Z",
		PRsFetchedAt: "2026-08-23T08:00:00Z",
		PRs:          []snapshot.PR{{Repo: "o/r", Number: 7, Title: "T", State: "open"}},
	}
	md := Markdown(d)
	if !strings.Contains(md, "STALE — fetched") {
		t.Errorf("ten-hour-old snapshot not marked stale:\n%s", md)
	}
}
