// Package athena implements daylog's gatekeeping subsystem: bounded curation,
// policy validation, persisted decisions, and replay-safe publication through the
// shared store. The configured model proposes; Athena's Go code validates and applies.
package athena

import (
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"strings"
	"unicode/utf8"
)

const Version = 2
const PolicyVersion = "athena-v2.5"

type Preference struct {
	Entry  string `json:"entry"`
	Choice string `json:"choice"`
	Reason string `json:"reason"`
}
type Input struct {
	Version         int                 `json:"version"`
	Policy          string              `json:"policy"`
	Candidates      []capture.Candidate `json:"candidates"`
	Evidence        []capture.Evidence  `json:"evidence"`
	Outcomes        []event.Entry       `json:"outcomes"`
	EditableTargets []event.Target      `json:"editable_targets"`
	Preferences     []Preference        `json:"preferences"`
	PreviousErrors  []string            `json:"previous_errors,omitempty"`
}
type Action struct {
	Kind       string         `json:"kind"`
	Candidates []string       `json:"candidates"`
	Evidence   []string       `json:"evidence"`
	Targets    []event.Target `json:"targets"`
	Type       string         `json:"type,omitempty"`
	Text       string         `json:"text,omitempty"`
	Details    string         `json:"details,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Refs       []string       `json:"refs"`
	Reason     string         `json:"reason"`
}
type Output struct {
	Version int      `json:"version"`
	Actions []Action `json:"actions"`
}
type Operation struct {
	ID     string       `json:"id"`
	Action Action       `json:"action"`
	Event  *event.Event `json:"event,omitempty"`
}
type Plan struct {
	EvidencePruned bool              `json:"evidence_pruned"`
	Version        int               `json:"version"`
	ID             string            `json:"id"`
	Mode           string            `json:"mode"`
	CreatedAt      string            `json:"created_at"`
	InputHash      string            `json:"input_hash"`
	Input          Input             `json:"input"`
	Model          string            `json:"model"`
	Operations     []Operation       `json:"operations"`
	Applied        map[string]string `json:"applied"`
	Status         string            `json:"status"`
	Error          string            `json:"error,omitempty"`
}

func Validate(in Input, out Output) error {
	if out.Version != Version || len(out.Actions) == 0 || len(out.Actions) > 32 {
		return fmt.Errorf("invalid Athena decision version/action count")
	}
	candidates := map[string]capture.Candidate{}
	for _, c := range in.Candidates {
		candidates[c.ID] = c
	}
	evidence := map[string]bool{}
	for _, e := range in.Evidence {
		evidence[e.ID] = true
	}
	targets := map[string]event.Entry{}
	for _, e := range in.Outcomes {
		targets[e.ID] = e
	}
	covered := map[string]string{}
	touched := map[string]bool{}
	for _, a := range out.Actions {
		if len(a.Candidates) == 0 || len(a.Candidates) > 16 || len(a.Evidence) > 32 || len(a.Refs) > 32 || strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 1024 {
			return fmt.Errorf("action missing citations/reason or exceeds bounds")
		}
		terminal := a.Kind == "skip" || a.Kind == "hold"
		allowedRefs := map[string]bool{}
		allowedEvidence := map[string]bool{}
		episode := ""
		repo := event.Context{}
		var primary capture.Candidate
		seen := map[string]bool{}
		for _, id := range a.Candidates {
			c, ok := candidates[id]
			if !ok || seen[id] {
				return fmt.Errorf("unknown/duplicate candidate %s", id)
			}
			seen[id] = true
			if prev := covered[id]; prev != "" && (terminal || prev == "skip" || prev == "hold") {
				return fmt.Errorf("conflicting dispositions for %s", id)
			}
			covered[id] = a.Kind
			if episode == "" {
				episode = c.Episode
				repo = c.Context
				primary = c
			} else if c.Episode != episode || !event.SameProject(c.Context, repo) || !event.SameDay(c.OccurredAt, primary.OccurredAt) {
				return fmt.Errorf("cannot combine unrelated episodes/repositories or occurrence days")
			}
			for _, r := range c.Refs {
				allowedRefs[r] = true
			}
			for _, e := range c.Evidence {
				allowedEvidence[e] = true
			}
		}
		for _, id := range a.Evidence {
			if !evidence[id] || !allowedEvidence[id] {
				return fmt.Errorf("invented or unrelated evidence %s", id)
			}
		}
		for _, t := range a.Targets {
			e, ok := targets[t.ID]
			if !ok || e.Revision != t.Revision {
				return fmt.Errorf("unknown target/revision")
			}
			if !event.SameProject(e.Context, repo) {
				return fmt.Errorf("cross-repository target")
			}
			for _, r := range e.Refs {
				allowedRefs[r] = true
			}
			if !terminal {
				if !event.SameDay(e.DisplayAt, primary.OccurredAt) {
					return fmt.Errorf("later-day milestones must be new dated deltas")
				}
				linked := false
				for _, id := range a.Candidates {
					linked = linked || linkedOutcome(candidates[id], e)
				}
				if !linked {
					return fmt.Errorf("target needs exact episode or shared PR/issue ref; use editable_targets")
				}
				if touched[t.ID] {
					return fmt.Errorf("conflicting target actions")
				}
				touched[t.ID] = true
				if event.Human(e.Source) || e.Pinned || e.Dismissed || e.MergedInto != "" || !event.Narrative(e.Type) {
					return fmt.Errorf("human-protected target")
				}
			}
		}
		for _, r := range a.Refs {
			if _, err := event.NormalizeRef(r, event.Repository{}); err != nil {
				return err
			}
			if !allowedRefs[r] {
				return fmt.Errorf("invented ref")
			}
		}
		switch a.Kind {
		case "publish":
			for _, e := range in.Outcomes {
				if e.Provenance == nil || (!e.Pinned && !e.Dismissed && e.MergedInto == "") {
					continue
				}
				for _, id := range a.Candidates {
					for _, previous := range e.Provenance.Candidates {
						if id == previous {
							return fmt.Errorf("replay cannot republish human-protected or merged input")
						}
					}
				}
			}
			if len(a.Targets) != 0 || !event.Narrative(a.Type) {
				return fmt.Errorf("invalid publish")
			}
		case "amend":
			if len(a.Targets) != 1 || !event.Narrative(a.Type) {
				return fmt.Errorf("invalid amend")
			}
		case "merge":
			if len(a.Targets) < 2 || len(a.Targets) > 16 || !event.Narrative(a.Type) {
				return fmt.Errorf("invalid merge")
			}
		case "skip", "hold":
			if a.Text != "" || a.Details != "" || len(a.Tags) != 0 || a.Type != "" || len(a.Refs) != 0 || len(a.Targets) > 1 {
				return fmt.Errorf("invalid disposition")
			}
			if a.Kind == "hold" && len(a.Targets) != 0 {
				return fmt.Errorf("hold cannot target")
			}
		default:
			return fmt.Errorf("unsupported editorial action %q", a.Kind)
		}
		if !terminal {
			if err := event.ValidateTLDR(a.Text); err != nil {
				return err
			}
			if utf8.RuneCountInString(a.Text) > event.MaxHeadlineChars {
				return fmt.Errorf("headline exceeds %d characters; move explanation into details", event.MaxHeadlineChars)
			}
			if err := event.ValidatePresentation(a.Details, a.Tags); err != nil {
				return err
			}
		}
	}
	if len(covered) != len(candidates) {
		return fmt.Errorf("every input must be accounted for")
	}
	return nil
}
