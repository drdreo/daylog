package athena

import (
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"strings"
)

// A shared typed artifact permits editorial consolidation; it does not assert
// that every outcome on a PR or issue is the same. The model still decides that.
func linkedOutcome(c capture.Candidate, e event.Entry) bool {
	if !event.SameProject(c.Context, e.Context) || !event.SameDay(c.OccurredAt, e.DisplayAt) {
		return false
	}
	if c.Episode != "" && e.Provenance != nil && c.Episode == e.Provenance.Episode {
		return true
	}
	for _, ref := range c.Refs {
		if _, err := event.NormalizeRef(ref, event.Repository{}); err != nil {
			continue
		}
		for _, previous := range e.Refs {
			if ref == previous {
				return true
			}
		}
	}
	return false
}

func editableTargets(in Input) []event.Target {
	out := []event.Target{}
	for _, e := range in.Outcomes {
		if event.Human(e.Source) || e.Pinned || e.Dismissed || e.MergedInto != "" || !event.Narrative(e.Type) {
			continue
		}
		for _, c := range in.Candidates {
			if linkedOutcome(c, e) {
				out = append(out, event.Target{ID: e.ID, Revision: e.Revision})
				break
			}
		}
	}
	return out
}

func editorialGroup(c capture.Candidate) string {
	// Candidate validation requires an RFC3339 occurrence timestamp. Its
	// captured calendar date, not the worker's time zone, owns the journal day.
	return c.Episode + "/" + strings.SplitN(c.OccurredAt, "T", 2)[0]
}
