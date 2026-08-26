package poll

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/snapshot"
	"github.com/drdreo/daylog/internal/store"
)

func snapWith(prs ...snapshot.PR) *snapshot.GHPRs {
	s := &snapshot.GHPRs{FetchedAt: "2026-08-23T10:00:00Z", PRs: map[string]snapshot.PR{}}
	for _, pr := range prs {
		s.PRs[pr.Ref] = pr
	}
	return s
}

func pr(ref, state, checks, review string) snapshot.PR {
	return snapshot.PR{Ref: ref, Repo: "owner/repo", Number: 142, Title: "Fix races",
		State: state, Checks: checks, Review: review}
}

func TestChecksFrom(t *testing.T) {
	cases := []struct {
		nodes []ghCheckNode
		want  string
	}{
		{nil, "none"},
		{[]ghCheckNode{{Status: "COMPLETED", Conclusion: "SUCCESS"}}, "passing"},
		{[]ghCheckNode{{Status: "COMPLETED", Conclusion: "SUCCESS"}, {Status: "COMPLETED", Conclusion: "FAILURE"}}, "failing"},
		{[]ghCheckNode{{Status: "IN_PROGRESS"}, {Status: "COMPLETED", Conclusion: "SUCCESS"}}, "pending"},
		{[]ghCheckNode{{State: "SUCCESS"}}, "passing"},
		{[]ghCheckNode{{State: "ERROR"}}, "failing"},
		{[]ghCheckNode{{State: "PENDING"}}, "pending"},
		{[]ghCheckNode{{Status: "COMPLETED", Conclusion: "SKIPPED"}, {Status: "COMPLETED", Conclusion: "NEUTRAL"}}, "passing"},
		{[]ghCheckNode{{Status: "QUEUED"}, {Status: "COMPLETED", Conclusion: "CANCELLED"}}, "failing"},
	}
	for i, c := range cases {
		if got := checksFrom(c.nodes); got != c.want {
			t.Errorf("case %d: checksFrom = %q, want %q", i, got, c.want)
		}
	}
}

// stubGH replaces the gh runner for one test. Keys are the leading args
// joined with spaces ("search prs", "pr view 1 --repo a/b").
func stubGH(t *testing.T, responses map[string]string, errs map[string]error) {
	t.Helper()
	orig := ghRun
	t.Cleanup(func() { ghRun = orig })
	ghRun = func(args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		for prefix, err := range errs {
			if strings.HasPrefix(joined, prefix) {
				return nil, err
			}
		}
		for prefix, body := range responses {
			if strings.HasPrefix(joined, prefix) {
				return []byte(body), nil
			}
		}
		return nil, fmt.Errorf("unexpected gh call: gh %s", joined)
	}
}

func tempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DAYLOG_DIR", dir)
	return dir
}

func viewJSON(state, checks string, review string) string {
	rollup := "[]"
	switch checks {
	case "passing":
		rollup = `[{"status":"COMPLETED","conclusion":"SUCCESS"}]`
	case "failing":
		rollup = `[{"status":"COMPLETED","conclusion":"FAILURE"}]`
	}
	return fmt.Sprintf(`{"state":%q,"isDraft":false,"title":"Fix races","url":"https://github.com/a/b/pull/1",
		"reviewDecision":%q,"statusCheckRollup":%s,"updatedAt":"2026-08-23T09:00:00Z"}`,
		state, review, rollup)
}

func TestRunGHFirstRunWritesSnapshotOnly(t *testing.T) {
	tempDataDir(t)
	stubGH(t, map[string]string{
		"search prs":           `[{"number":1,"repository":{"nameWithOwner":"a/b"}}]`,
		"pr view 1 --repo a/b": viewJSON("OPEN", "failing", "CHANGES_REQUESTED"),
	}, nil)

	if err := RunGH(io.Discard, io.Discard, false, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshot.LoadGHPRs()
	if err != nil || snap == nil {
		t.Fatalf("snapshot not written: %v", err)
	}
	got, ok := snap.PRs["gh:pr:a/b#1"]
	if !ok {
		t.Fatalf("snapshot missing PR, has %v", snap.PRs)
	}
	if got.State != "open" || got.Checks != "failing" || got.Review != "changes_requested" {
		t.Errorf("PR = %+v", got)
	}
	if evs, _ := store.ReadAll(); len(evs) != 0 {
		t.Fatalf("GitHub poll wrote %d narrative events", len(evs))
	}
}

func TestRunGHWorkflowChangesStaySnapshotOnly(t *testing.T) {
	tempDataDir(t)
	prev := snapWith(snapshot.PR{Ref: "gh:pr:a/b#1", Repo: "a/b", Number: 1,
		Title: "Fix races", State: "open", Checks: "passing", Review: "approved"})
	if err := snapshot.SaveGHPRs(prev); err != nil {
		t.Fatal(err)
	}
	stubGH(t, map[string]string{
		"search prs":           `[{"number":1,"repository":{"nameWithOwner":"a/b"}}]`,
		"pr view 1 --repo a/b": viewJSON("OPEN", "failing", "CHANGES_REQUESTED"),
	}, nil)

	if err := RunGH(io.Discard, io.Discard, false, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	snap, _ := snapshot.LoadGHPRs()
	got := snap.PRs["gh:pr:a/b#1"]
	if got.Checks != "failing" || got.Review != "changes_requested" {
		t.Fatalf("snapshot did not refresh workflow state: %+v", got)
	}
	if evs, _ := store.ReadAll(); len(evs) != 0 {
		t.Fatalf("workflow refresh wrote %d narrative events", len(evs))
	}
}

func TestRunGHDropsMergedPRWithoutNarratingIt(t *testing.T) {
	tempDataDir(t)
	prev := snapWith(snapshot.PR{Ref: "gh:pr:a/b#1", Repo: "a/b", Number: 1,
		Title: "Fix races", State: "open", Checks: "passing", Review: "approved"})
	if err := snapshot.SaveGHPRs(prev); err != nil {
		t.Fatal(err)
	}
	stubGH(t, map[string]string{"search prs": `[]`}, nil)

	if err := RunGH(io.Discard, io.Discard, false, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if evs, err := store.ReadAll(); err != nil || len(evs) != 0 {
		t.Fatalf("events = %d (%v), want none", len(evs), err)
	}
	snap, _ := snapshot.LoadGHPRs()
	if len(snap.PRs) != 0 {
		t.Errorf("snapshot = %+v, want merged PR removed", snap.PRs)
	}
}

func TestRunGHFetchFailureKeepsSnapshotAndExitsZero(t *testing.T) {
	tempDataDir(t)
	prev := snapWith(pr("gh:pr:a/b#1", "open", "passing", "none"))
	if err := snapshot.SaveGHPRs(prev); err != nil {
		t.Fatal(err)
	}
	stubGH(t, nil, map[string]error{"search prs": fmt.Errorf("no network")})

	if err := RunGH(io.Discard, io.Discard, false, time.Now(), ""); err != nil {
		t.Fatalf("no network must not be an error state, got %v", err)
	}
	snap, _ := snapshot.LoadGHPRs()
	if snap == nil || snap.FetchedAt != prev.FetchedAt {
		t.Fatalf("old snapshot not preserved: %+v", snap)
	}
	if evs, _ := store.ReadAll(); len(evs) != 0 {
		t.Fatalf("failed fetch fabricated %d transitions", len(evs))
	}
}

func TestRunGHPartialFetchFailureSkipsEverything(t *testing.T) {
	tempDataDir(t)
	prev := snapWith(snapshot.PR{Ref: "gh:pr:a/b#1", Repo: "a/b", Number: 1,
		Title: "Fix races", State: "open", Checks: "passing"})
	if err := snapshot.SaveGHPRs(prev); err != nil {
		t.Fatal(err)
	}
	stubGH(t, map[string]string{
		"search prs": `[{"number":1,"repository":{"nameWithOwner":"a/b"}}]`,
	}, map[string]error{"pr view 1 --repo a/b": fmt.Errorf("api hiccup")})

	if err := RunGH(io.Discard, io.Discard, false, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	snap, _ := snapshot.LoadGHPRs()
	if snap.FetchedAt != prev.FetchedAt {
		t.Fatal("partial fetch must not replace the snapshot")
	}
	if evs, _ := store.ReadAll(); len(evs) != 0 {
		t.Fatal("partial fetch must not write narrative events")
	}
}

func TestRunGHDryRunWritesNothing(t *testing.T) {
	dir := tempDataDir(t)
	prev := snapWith(snapshot.PR{Ref: "gh:pr:a/b#1", Repo: "a/b", Number: 1,
		Title: "Fix races", State: "open", Checks: "passing"})
	if err := snapshot.SaveGHPRs(prev); err != nil {
		t.Fatal(err)
	}
	stubGH(t, map[string]string{
		"search prs":           `[{"number":1,"repository":{"nameWithOwner":"a/b"}}]`,
		"pr view 1 --repo a/b": viewJSON("OPEN", "failing", "CHANGES_REQUESTED"),
	}, nil)

	var out strings.Builder
	if err := RunGH(&out, io.Discard, true, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "would snapshot 1 open PRs") {
		t.Errorf("dry-run output = %q", out.String())
	}
	if evs, _ := store.ReadAll(); len(evs) != 0 {
		t.Fatal("dry run wrote events")
	}
	snap, _ := snapshot.LoadGHPRs()
	if snap.FetchedAt != prev.FetchedAt {
		t.Fatal("dry run replaced the snapshot")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "events")); len(entries) != 0 {
		t.Fatal("dry run touched the events dir")
	}
}

func TestFetchParsesSearchAndView(t *testing.T) {
	tempDataDir(t)
	stubGH(t, map[string]string{
		"search prs":           `[{"number":7,"repository":{"nameWithOwner":"o/r"}}]`,
		"pr view 7 --repo o/r": `{"state":"OPEN","isDraft":true,"title":"T","url":"u","reviewDecision":"","statusCheckRollup":[{"status":"IN_PROGRESS"}],"updatedAt":"x"}`,
	}, nil)
	now, _ := time.Parse(time.RFC3339, "2026-08-23T12:00:00Z")
	cur, _, err := fetchGHPRs(now, ownerFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if cur.FetchedAt != "2026-08-23T12:00:00Z" {
		t.Errorf("fetched_at = %q", cur.FetchedAt)
	}
	got := cur.PRs["gh:pr:o/r#7"]
	want := snapshot.PR{Ref: "gh:pr:o/r#7", Repo: "o/r", Number: 7, Title: "T", URL: "u",
		State: "open", Draft: true, Checks: "pending", Review: "none", UpdatedAt: "x"}
	if got != want {
		t.Errorf("PR = %+v, want %+v", got, want)
	}
}

func TestParseOwnerFilter(t *testing.T) {
	// \r included: an EnvironmentFile written with CRLF must not turn the
	// whole poll into a hard error over an invisible character.
	f, err := parseOwnerFilter(" LovableLabs, !oldorg\r  @me ")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.include) != "[lovablelabs @me]" || fmt.Sprint(f.exclude) != "[oldorg]" {
		t.Fatalf("filter = %+v", f)
	}
	if f.empty() {
		t.Error("filter with entries reports empty")
	}
	if empty, _ := parseOwnerFilter(""); !empty.empty() {
		t.Error("blank spec must mean unfiltered")
	}
	for _, bad := range []string{"has space/slash", "!", "org/repo", "bad_name"} {
		if _, err := parseOwnerFilter(bad); err == nil {
			t.Errorf("parseOwnerFilter(%q) accepted a malformed entry", bad)
		}
	}
}

func TestOwnerFilterAllows(t *testing.T) {
	cases := []struct {
		spec, repo string
		want       bool
	}{
		{"", "any/repo", true},
		{"lovablelabs", "lovablelabs/app", true},
		{"lovablelabs", "LovableLabs/App", true}, // owners are case-insensitive
		{"lovablelabs", "otherco/app", false},
		{"lovablelabs,drdreo", "drdreo/daylog", true},
		{"!otherco", "lovablelabs/app", true},
		{"!otherco", "otherco/app", false},
		{"lovablelabs,!lovablelabs", "lovablelabs/app", false}, // exclude wins
	}
	for _, c := range cases {
		f, err := parseOwnerFilter(c.spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.allows(c.repo); got != c.want {
			t.Errorf("%q allows %q = %v, want %v", c.spec, c.repo, got, c.want)
		}
	}
}

func TestFetchNarrowsSearchAndDropsOutOfScopePRs(t *testing.T) {
	tempDataDir(t)
	var searched string
	orig := ghRun
	t.Cleanup(func() { ghRun = orig })
	ghRun = func(args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "search prs"):
			searched = joined
			// The search is asked to narrow, but answer broadly anyway:
			// the local filter must hold on its own.
			return []byte(`[{"number":1,"repository":{"nameWithOwner":"work/app"}},
				{"number":2,"repository":{"nameWithOwner":"personal/toy"}}]`), nil
		case strings.HasPrefix(joined, "pr view 1 --repo work/app"):
			return []byte(viewJSON("OPEN", "passing", "")), nil
		}
		return nil, fmt.Errorf("unexpected gh call: gh %s", joined)
	}

	filter, err := parseOwnerFilter("work")
	if err != nil {
		t.Fatal(err)
	}
	cur, _, err := fetchGHPRs(time.Now(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searched, "--owner=work") {
		t.Errorf("search args = %q, want a server-side --owner narrowing", searched)
	}
	if len(cur.PRs) != 1 || cur.PRs["gh:pr:work/app#1"].State != "open" {
		t.Fatalf("snapshot = %+v, want only the in-scope PR", cur.PRs)
	}
}

func TestFetchResolvesSelfToken(t *testing.T) {
	tempDataDir(t)
	stubGH(t, map[string]string{
		"api user":                       "drdreo\n",
		"search prs":                     `[{"number":1,"repository":{"nameWithOwner":"drdreo/daylog"}}]`,
		"pr view 1 --repo drdreo/daylog": viewJSON("OPEN", "passing", ""),
	}, nil)

	filter, err := parseOwnerFilter("@me")
	if err != nil {
		t.Fatal(err)
	}
	cur, _, err := fetchGHPRs(time.Now(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cur.PRs["gh:pr:drdreo/daylog#1"]; !ok {
		t.Fatalf("snapshot = %+v, want the authenticated user's PR", cur.PRs)
	}
}

func TestRunGHRejectsMalformedFilterBeforeTouchingDisk(t *testing.T) {
	tempDataDir(t)
	stubGH(t, nil, nil) // any gh call would fail the test as unexpected
	if err := RunGH(io.Discard, io.Discard, false, time.Now(), "not_a_login"); err == nil {
		t.Fatal("a malformed owner filter must be an error, not a silent empty poll")
	}
	if snap, _ := snapshot.LoadGHPRs(); snap != nil {
		t.Fatal("rejected config wrote a snapshot")
	}
}

func TestRunGHOutOfScopePRDropsWithoutNarrativeEvent(t *testing.T) {
	tempDataDir(t)
	prev := snapWith(snapshot.PR{Ref: "gh:pr:otherco/app#1", Repo: "otherco/app",
		Number: 1, Title: "Fix races", State: "open", Checks: "passing"})
	if err := snapshot.SaveGHPRs(prev); err != nil {
		t.Fatal(err)
	}
	stubGH(t, map[string]string{"search prs": `[]`}, nil)

	if err := RunGH(io.Discard, io.Discard, false, time.Now(), "work"); err != nil {
		t.Fatal(err)
	}
	if evs, _ := store.ReadAll(); len(evs) != 0 {
		t.Fatalf("narrowing the filter wrote %d narrative events", len(evs))
	}
	snap, _ := snapshot.LoadGHPRs()
	if len(snap.PRs) != 0 {
		t.Fatalf("snapshot = %+v, want the out-of-scope PR dropped", snap.PRs)
	}
}

func TestRunGHSummaryReportsResolvedSelf(t *testing.T) {
	tempDataDir(t)
	stubGH(t, map[string]string{
		"api user":                       "drdreo\n",
		"search prs":                     `[{"number":1,"repository":{"nameWithOwner":"drdreo/daylog"}}]`,
		"pr view 1 --repo drdreo/daylog": viewJSON("OPEN", "passing", ""),
	}, nil)

	var out strings.Builder
	if err := RunGH(&out, io.Discard, false, time.Now(), "@me"); err != nil {
		t.Fatal(err)
	}
	// The scope line is where a user checks which account gh actually used
	// (say, after `gh auth switch`), so it must show the login, not the token.
	if !strings.Contains(out.String(), "owners: drdreo") {
		t.Fatalf("summary = %q, want the resolved login", out.String())
	}
}
