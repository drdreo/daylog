package view

import (
	"sort"
	"strings"

	"github.com/drdreo/daylog/internal/snapshot"
)

// JoinGH is the read-time join between the narrative and the now (§3):
// entries referencing a PR gain its live status from the snapshot, and the
// day view gains the full list of open PRs. Ref equality is the whole join —
// no producer or consumer knows anything else about the other side (§4.3).
func JoinGH(d *Day, snap *snapshot.GHPRs) {
	if snap == nil {
		return
	}
	d.PRsFetchedAt = snap.FetchedAt

	decorate := func(entries []Entry) {
		for i := range entries {
			for _, ref := range entries[i].Refs {
				if pr, ok := snap.PRs[ref]; ok {
					entries[i].PR = &pr
					break
				}
			}
		}
	}
	decorate(d.Entries)
	decorate(d.OpenTodos)
	decorate(d.AgentInbox)

	for _, pr := range snap.PRs {
		if pr.State == "open" {
			d.PRs = append(d.PRs, pr)
		}
	}
	sort.Slice(d.PRs, func(i, j int) bool {
		if d.PRs[i].Repo != d.PRs[j].Repo {
			return d.PRs[i].Repo < d.PRs[j].Repo
		}
		return d.PRs[i].Number < d.PRs[j].Number
	})
}

// prStatusLabel compresses a PR's live state into the short annotation shown
// next to entries and in the Open PRs section.
func prStatusLabel(pr *snapshot.PR) string {
	if pr.State != "open" {
		return pr.State
	}
	var parts []string
	if pr.Draft {
		parts = append(parts, "draft")
	}
	if pr.Checks != "none" {
		parts = append(parts, "checks "+pr.Checks)
	}
	switch pr.Review {
	case "approved", "review_required":
		parts = append(parts, strings.ReplaceAll(pr.Review, "_", " "))
	case "changes_requested":
		parts = append(parts, "changes requested")
	}
	if len(parts) == 0 {
		return "open"
	}
	return strings.Join(parts, " · ")
}
