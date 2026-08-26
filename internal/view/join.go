package view

import (
	"sort"
	"strings"

	"github.com/drdreo/daylog/internal/snapshot"
)

// JoinGH adds the current open-PR snapshot to the day view (§3). Live PR
// status stays in that separate collection rather than decorating narrative
// entries: the log records work outcomes, while the Open PRs section owns
// checks, review state, and links.
func JoinGH(d *Day, snap *snapshot.GHPRs) {
	if snap == nil {
		return
	}
	d.PRsFetchedAt = snap.FetchedAt

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

// prStatusLabel compresses a PR's live state for the Open PRs section.
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
