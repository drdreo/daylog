// Package poll implements pollers: producers that snapshot external state,
// diff it against the previous snapshot, and narrate meaningful changes as
// transition events (§6). The GitHub poller establishes the pattern every
// future integration reuses.
//
// Two invariants hold throughout: a failed or partial fetch skips the diff
// entirely rather than fabricate transitions, and a poller with no network
// exits 0 and leaves the old snapshot with its honest fetched_at.
package poll

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/snapshot"
	"github.com/drdreo/daylog/internal/store"
)

// ghRun executes the gh CLI. Pollers shell out to the provider's own tool
// rather than speaking HTTP: auth comes for free, and gh absent or
// unauthenticated is an honest skip, not a corruption (§6). A var so tests
// can stub it.
var ghRun = func(args ...string) ([]byte, error) {
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gh %s: %s", args[0], firstLine(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh %s: %w", args[0], err)
	}
	return out, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// ownerFilter narrows the poller to certain repository owners (§6). One
// machine is rarely one context: a work laptop wants only `lovablelabs`
// PRs, a home machine only the ones under the user's own account. Entries
// are logins, case-insensitive; a leading `!` excludes instead of includes;
// `@me` stands for the authenticated user, so the same spec is portable.
// An empty filter tracks every owner — the original behaviour.
type ownerFilter struct {
	include []string // lowercased logins; empty means "any owner not excluded"
	exclude []string
}

const selfToken = "@me"

// parseOwnerFilter reads the spec from --owner / $DAYLOG_GH_OWNERS:
// entries separated by commas or whitespace, e.g. "lovablelabs, !oldorg".
// A malformed entry is an error rather than a silent no-match — a typo that
// quietly tracked nothing would look exactly like a quiet day.
func parseOwnerFilter(spec string) (ownerFilter, error) {
	var f ownerFilter
	for _, raw := range strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		entry := raw
		negated := strings.HasPrefix(entry, "!")
		entry = strings.ToLower(strings.TrimPrefix(entry, "!"))
		if err := validOwner(entry); err != nil {
			return ownerFilter{}, fmt.Errorf("owner filter %q: %w", raw, err)
		}
		if negated {
			f.exclude = append(f.exclude, entry)
		} else {
			f.include = append(f.include, entry)
		}
	}
	return f, nil
}

// validOwner accepts a GitHub login (alphanumerics and hyphens) or @me.
func validOwner(s string) error {
	if s == selfToken {
		return nil
	}
	if s == "" {
		return errors.New("empty owner")
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return errors.New("not a github owner login (letters, digits, hyphens, or @me)")
	}
	return nil
}

func (f ownerFilter) empty() bool { return len(f.include) == 0 && len(f.exclude) == 0 }

// allows reports whether a repo ("owner/name") is in scope. Excludes win over
// includes; with no includes, everything not excluded is in scope.
func (f ownerFilter) allows(repo string) bool {
	owner := strings.ToLower(repo)
	if i := strings.Index(owner, "/"); i >= 0 {
		owner = owner[:i]
	}
	for _, e := range f.exclude {
		if e == owner {
			return false
		}
	}
	if len(f.include) == 0 {
		return true
	}
	for _, i := range f.include {
		if i == owner {
			return true
		}
	}
	return false
}

// resolveSelf expands the @me token to the authenticated login. Costs one
// extra gh call, and only when the spec actually uses the token.
func (f ownerFilter) resolveSelf() (ownerFilter, error) {
	if !f.usesSelf() {
		return f, nil
	}
	out, err := ghRun("api", "user", "--jq", ".login")
	if err != nil {
		return ownerFilter{}, err
	}
	login := strings.ToLower(strings.TrimSpace(string(out)))
	if err := validOwner(login); err != nil || login == selfToken {
		return ownerFilter{}, fmt.Errorf("resolve %s: gh returned %q", selfToken, strings.TrimSpace(string(out)))
	}
	swap := func(list []string) []string {
		out := make([]string, len(list))
		for i, o := range list {
			if o == selfToken {
				o = login
			}
			out[i] = o
		}
		return out
	}
	return ownerFilter{include: swap(f.include), exclude: swap(f.exclude)}, nil
}

func (f ownerFilter) usesSelf() bool {
	for _, list := range [][]string{f.include, f.exclude} {
		for _, o := range list {
			if o == selfToken {
				return true
			}
		}
	}
	return false
}

// String describes the filter for the poll summary line.
func (f ownerFilter) String() string {
	var parts []string
	parts = append(parts, f.include...)
	for _, e := range f.exclude {
		parts = append(parts, "!"+e)
	}
	return strings.Join(parts, ", ")
}

// Transition is one meaningful change observed between two snapshots.
type Transition struct {
	Ref  string
	Kind string // pr_merged | pr_closed | checks_failing | checks_passing | review_approved | review_changes_requested
	TLDR string
	Meta map[string]any
}

// RunGH performs one poll cycle: fetch, diff, emit, save. It is the body of
// `daylog poll gh` and of the systemd timer unit. ownerSpec narrows which
// repository owners are tracked (see ownerFilter); empty means all.
func RunGH(stdout, stderr io.Writer, dryRun bool, now time.Time, ownerSpec string) error {
	filter, err := parseOwnerFilter(ownerSpec)
	if err != nil {
		return err // a misconfigured filter is the caller's bug, not a skip
	}

	prev, err := snapshot.LoadGHPRs()
	if err != nil {
		// A corrupt cache is discardable: treat as a first run (baseline
		// only, no diff) rather than fabricating transitions against it.
		fmt.Fprintf(stderr, "warning: previous snapshot unreadable (%v); establishing a fresh baseline\n", err)
		prev = nil
	}

	cur, filter, err := fetchGHPRs(prev, now, filter)
	if err != nil {
		fmt.Fprintf(stderr, "gh poll: fetch failed, keeping previous snapshot: %v\n", err)
		return nil // no network / no gh is not an error state (§6)
	}

	var transitions []Transition
	if prev != nil {
		transitions = diffGHPRs(prev, cur)
	}

	for _, t := range transitions {
		e := transitionEvent(t, now)
		if dryRun {
			fmt.Fprintf(stdout, "would log: %s (%s)\n", t.TLDR, t.Ref)
			continue
		}
		// Events before snapshot: a crash between the two re-emits
		// duplicates next run, which beats losing the narrative.
		if err := store.Append(e); err != nil {
			return fmt.Errorf("append transition: %w", err)
		}
	}
	if !dryRun {
		if err := snapshot.SaveGHPRs(cur); err != nil {
			return err
		}
	}

	scope := ""
	if !filter.empty() {
		scope = fmt.Sprintf(" (owners: %s)", filter)
	}
	if prev == nil {
		fmt.Fprintf(stdout, "gh poll: baseline snapshot of %d PRs%s (no transitions on first run)\n", len(cur.PRs), scope)
	} else {
		fmt.Fprintf(stdout, "gh poll: %d PRs tracked%s, %d transitions\n", len(cur.PRs), scope, len(transitions))
	}
	return nil
}

// ghSearchItem is one result of `gh search prs`.
type ghSearchItem struct {
	Number     int `json:"number"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// ghPRView is the per-PR detail from `gh pr view --json`.
type ghPRView struct {
	State             string        `json:"state"` // OPEN | MERGED | CLOSED
	IsDraft           bool          `json:"isDraft"`
	Title             string        `json:"title"`
	URL               string        `json:"url"`
	ReviewDecision    string        `json:"reviewDecision"` // APPROVED | CHANGES_REQUESTED | REVIEW_REQUIRED | ""
	StatusCheckRollup []ghCheckNode `json:"statusCheckRollup"`
	UpdatedAt         string        `json:"updatedAt"`
}

// ghCheckNode covers both shapes in statusCheckRollup: CheckRun
// (status/conclusion) and legacy StatusContext (state).
type ghCheckNode struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// fetchGHPRs builds the new snapshot: every PR currently open and authored
// by the user, plus every PR the previous snapshot still thought was open —
// re-fetching the latter is how a merge or close gets observed. Any error
// aborts the whole fetch: a partial picture must never reach the diff.
// The owner filter applies to both halves, so a PR that falls out of scope
// simply stops being tracked; it is never narrated as closed.
// It returns the resolved filter (with @me expanded) so callers can report
// the scope they actually polled rather than the token they were given.
func fetchGHPRs(prev *snapshot.GHPRs, now time.Time, filter ownerFilter) (*snapshot.GHPRs, ownerFilter, error) {
	filter, err := filter.resolveSelf()
	if err != nil {
		return nil, filter, err
	}

	args := []string{"search", "prs", "--author=@me", "--state=open",
		"--json", "number,repository", "--limit", "200"}
	for _, owner := range filter.include {
		args = append(args, "--owner="+owner) // narrow server-side where we can
	}
	out, err := ghRun(args...)
	if err != nil {
		return nil, filter, err
	}
	var open []ghSearchItem
	if err := json.Unmarshal(out, &open); err != nil {
		return nil, filter, fmt.Errorf("parse gh search output: %w", err)
	}

	type key struct {
		repo   string
		number int
	}
	tracked := map[key]bool{}
	var order []key
	track := func(k key) {
		if !tracked[k] {
			tracked[k] = true
			order = append(order, k)
		}
	}
	for _, it := range open {
		if !filter.allows(it.Repository.NameWithOwner) {
			continue
		}
		track(key{it.Repository.NameWithOwner, it.Number})
	}
	if prev != nil {
		for _, pr := range prev.PRs {
			if pr.State == "open" && filter.allows(pr.Repo) {
				track(key{pr.Repo, pr.Number})
			}
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].repo != order[j].repo {
			return order[i].repo < order[j].repo
		}
		return order[i].number < order[j].number
	})

	cur := &snapshot.GHPRs{
		FetchedAt: now.Format(time.RFC3339),
		PRs:       map[string]snapshot.PR{},
	}
	for _, k := range order {
		out, err := ghRun("pr", "view", fmt.Sprint(k.number), "--repo", k.repo,
			"--json", "state,isDraft,title,url,reviewDecision,statusCheckRollup,updatedAt")
		if err != nil {
			return nil, filter, err
		}
		var v ghPRView
		if err := json.Unmarshal(out, &v); err != nil {
			return nil, filter, fmt.Errorf("parse gh pr view output for %s#%d: %w", k.repo, k.number, err)
		}
		ref := fmt.Sprintf("gh:pr:%s#%d", k.repo, k.number)
		cur.PRs[ref] = snapshot.PR{
			Ref:       ref,
			Repo:      k.repo,
			Number:    k.number,
			Title:     v.Title,
			URL:       v.URL,
			State:     strings.ToLower(v.State),
			Draft:     v.IsDraft,
			Checks:    checksFrom(v.StatusCheckRollup),
			Review:    reviewFrom(v.ReviewDecision),
			UpdatedAt: v.UpdatedAt,
		}
	}
	return cur, filter, nil
}

// checksFrom folds the rollup into one word: any red check makes the PR
// failing, else any unfinished one makes it pending, else passing.
func checksFrom(nodes []ghCheckNode) string {
	if len(nodes) == 0 {
		return "none"
	}
	pending := false
	for _, n := range nodes {
		switch n.Conclusion {
		case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "failing"
		}
		switch n.State {
		case "FAILURE", "ERROR":
			return "failing"
		case "PENDING", "EXPECTED":
			pending = true
		}
		if n.Status != "" && n.Status != "COMPLETED" {
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "passing"
}

func reviewFrom(decision string) string {
	if decision == "" {
		return "none"
	}
	return strings.ToLower(decision)
}

// diffGHPRs narrates the meaningful changes between two snapshots (§6):
// merged, closed without merge, checks flipped red or back to green, and
// review decisions. A PR appearing is not a transition — opening it was the
// producer's own action, already narrated by its work entry. Neither is a
// pending checks state: only settled flips are worth the human's attention.
func diffGHPRs(old, cur *snapshot.GHPRs) []Transition {
	var refs []string
	for ref := range cur.PRs {
		if _, existed := old.PRs[ref]; existed {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)

	var out []Transition
	for _, ref := range refs {
		o, n := old.PRs[ref], cur.PRs[ref]
		add := func(kind, tldr string, meta map[string]any) {
			if meta == nil {
				meta = map[string]any{}
			}
			meta["kind"] = kind
			if n.URL != "" {
				meta["url"] = n.URL
			}
			out = append(out, Transition{Ref: ref, Kind: kind, TLDR: tldr, Meta: meta})
		}
		title := clip(n.Title, 240)

		if o.State == "open" && n.State != "open" {
			switch n.State {
			case "merged":
				add("pr_merged", "PR merged: "+title, nil)
			case "closed":
				add("pr_closed", "PR closed without merge: "+title, nil)
			}
			continue // a finished PR's checks and reviews no longer matter
		}
		if n.State != "open" {
			continue
		}
		if n.Checks == "failing" && o.Checks != "failing" {
			add("checks_failing", "Checks failing: "+title,
				map[string]any{"from": o.Checks, "to": n.Checks})
		}
		if n.Checks == "passing" && o.Checks == "failing" {
			add("checks_passing", "Checks green again: "+title,
				map[string]any{"from": o.Checks, "to": n.Checks})
		}
		if n.Review != o.Review {
			switch n.Review {
			case "approved":
				add("review_approved", "Review approved: "+title,
					map[string]any{"from": o.Review, "to": n.Review})
			case "changes_requested":
				add("review_changes_requested", "Changes requested: "+title,
					map[string]any{"from": o.Review, "to": n.Review})
			}
		}
	}
	return out
}

// transitionEvent shapes a Transition as an event through the same schema
// every producer uses. Ctx stays empty: a timer has no meaningful cwd.
func transitionEvent(t Transition, now time.Time) event.Event {
	return event.Event{
		ID:     event.NewID(now),
		TS:     now.Format(time.RFC3339),
		Host:   hostname(),
		Source: "poller:gh",
		Type:   event.TypeTransition,
		TLDR:   t.TLDR,
		Refs:   []string{t.Ref},
		Meta:   t.Meta,
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// clip bounds a title so the composed tldr stays under the 280-char write
// limit. Clipping here is honest — the full title lives in GitHub, the
// event only points at it.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
