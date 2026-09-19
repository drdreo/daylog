package athena

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
)

type personalityCase struct {
	Name            string      `json:"name"`
	Report          string      `json:"report"`
	Refs            []string    `json:"refs"`
	Existing        string      `json:"existing"`
	ExistingDetails string      `json:"existing_details"`
	Preference      *Preference `json:"preference"`
	Want            string      `json:"want"`
	Example         string      `json:"example"`
	Review          string      `json:"review"`
}

func personalityCases(t *testing.T) []personalityCase {
	t.Helper()
	b, err := os.ReadFile("../../testdata/gatekeeper/personality.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []personalityCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no personality eval cases")
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if tc.Name == "" || seen[tc.Name] || tc.Report == "" || tc.Review == "" {
			t.Fatalf("invalid or duplicate eval case: %+v", tc)
		}
		seen[tc.Name] = true
		if !slices.Contains([]string{"publish", "skip", "hold"}, tc.Want) {
			t.Fatalf("%s: unsupported expected disposition %q", tc.Name, tc.Want)
		}
		if tc.Want == "publish" && tc.Example == "" {
			t.Fatalf("%s: publication needs an illustrative headline", tc.Name)
		}
		if tc.Preference != nil && (tc.Existing == "" || tc.Preference.Choice != "skip" || tc.Preference.Reason == "") {
			t.Fatalf("%s: preference needs its source outcome, skip choice, and reason", tc.Name)
		}
	}
	return cases
}

func personalityInput(t *testing.T, tc personalityCase) (*Worker, Input) {
	t.Helper()
	w, _, c := fixture(t, "live")
	c.Text, c.Refs, c.Terminal = tc.Report, tc.Refs, "unknown"
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	if tc.Existing != "" {
		prior := event.Entry{Event: event.Event{
			ID: event.NewID(w.now()), Source: "agent:pi", Type: "work",
			TLDR: tc.Existing, Details: tc.ExistingDetails, Context: c.Context,
			Provenance: &event.Provenance{Episode: c.Episode},
		}, Revision: 1, DisplayAt: c.OccurredAt}
		in.Outcomes = []event.Entry{prior}
		in.EditableTargets = editableTargets(in)
		if tc.Preference != nil {
			p := *tc.Preference
			p.Entry = prior.ID
			in.Preferences = []Preference{p}
		}
	}
	return w, in
}

// These checks are deliberately mechanical. The isolated judge assesses meaning;
// neither exact jokes nor keyword counts grade voice. Human review remains useful.
func checkPersonalityOutput(tc personalityCase, in Input, out Output) error {
	if err := Validate(in, out); err != nil {
		return err
	}
	if len(out.Actions) != 1 {
		return fmt.Errorf("want one disposition, got %d", len(out.Actions))
	}
	a := out.Actions[0]
	if a.Kind != tc.Want {
		return fmt.Errorf("want %s, got %s", tc.Want, a.Kind)
	}
	if a.Kind == "publish" {
		for _, ref := range tc.Refs {
			if !slices.Contains(a.Refs, ref) {
				return fmt.Errorf("lost supplied reference %s", ref)
			}
		}
	}
	return nil
}

// Default tests validate the fixtures and checks without invoking a model.
// Examples are illustrative headlines, not exact-match answers or live results.
func TestPersonalityEvalFixtures(t *testing.T) {
	if !strings.Contains(Policy, "under policy "+PolicyVersion+".") {
		t.Fatal("embedded policy and saved-plan policy version disagree")
	}
	for _, tc := range personalityCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			_, in := personalityInput(t, tc)
			a := Action{Kind: tc.Want, Candidates: []string{in.Candidates[0].ID}, Reason: "synthetic fixture"}
			if tc.Want == "publish" {
				a.Type, a.Text, a.Refs = "work", tc.Example, tc.Refs
			}
			out := Output{Version: Version, Actions: []Action{a}}
			if err := checkPersonalityOutput(tc, in, out); err != nil {
				t.Fatal(err)
			}
			wrong := out
			wrong.Actions = []Action{{Kind: "hold", Candidates: a.Candidates, Reason: "wrong disposition"}}
			if tc.Want == "hold" {
				wrong.Actions[0].Kind = "skip"
			}
			if err := checkPersonalityOutput(tc, in, wrong); err == nil {
				t.Fatal("accepted the wrong disposition")
			}
			if len(tc.Refs) > 0 {
				if tc.Want == "publish" {
					out.Actions[0].Refs = nil
					if err := checkPersonalityOutput(tc, in, out); err == nil {
						t.Fatal("accepted a dropped publication reference")
					}
				} else {
					out.Actions[0].Refs = tc.Refs
					if err := checkPersonalityOutput(tc, in, out); err == nil {
						t.Fatal("accepted references on a skip/hold disposition")
					}
				}
			}
		})
	}
}

// Uses the existing opt-in smoke-test runner and only synthetic temporary data.
// A green test means mechanical checks and the fallible LLM rubric passed.
// Inspect the quoted reasons; this is not factual proof or a quality guarantee.
func TestOptInAthenaPersonality(t *testing.T) {
	if os.Getenv("DAYLOG_LUNA_SMOKE") != "1" {
		t.Skip("synthetic model evaluation is opt-in; see docs/athena-evals.md")
	}
	binary, agentDir := os.Getenv("DAYLOG_SMOKE_PI"), os.Getenv("DAYLOG_SMOKE_PI_AGENT_DIR")
	if binary == "" || agentDir == "" {
		t.Fatal("set DAYLOG_SMOKE_PI and DAYLOG_SMOKE_PI_AGENT_DIR")
	}
	cases := personalityCases(t)
	if !t.Run("judge-rejects-bad-output", func(t *testing.T) {
		var tc personalityCase
		for _, candidate := range cases {
			if candidate.Name == "failed-fix-stays-failed" {
				tc = candidate
			}
		}
		if tc.Name == "" {
			t.Fatal("missing failed-fix fixture for judge sanity check")
		}
		w, in := personalityInput(t, tc)
		w.Config.Runner.Binary, w.Config.Runner.AgentDir, w.Config.Runner.Path = binary, agentDir, os.Getenv("PATH")
		bad := Output{Version: Version, Actions: []Action{{
			Kind: "publish", Candidates: []string{in.Candidates[0].ID}, Type: "work",
			Text:    "All save-loss bugs fixed and deployed",
			Details: "Every test passed. Took you long enough, genius.", Reason: "Fix deployed successfully.",
		}}}
		if err := Validate(in, bad); err != nil {
			t.Fatal("bad wording must pass structure checks so the judge tests semantics:", err)
		}
		verdict, err := runPersonalityJudge(t, w.Config.Runner, in, bad)
		if err != nil {
			t.Fatalf("judge error (not a quality verdict): %v", err)
		}
		b, err := json.Marshal(verdict)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("DELIBERATELY BAD OUTPUT JUDGMENT: %s", b)
		if *verdict.Fidelity.Pass || *verdict.Voice.Pass {
			t.Fatal("judge must reject fabricated success and the personal insult")
		}
	}) {
		t.Fatal("judge sanity check failed; not spending calls grading generated outputs")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			w, in := personalityInput(t, tc)
			w.Config.Runner.Binary, w.Config.Runner.AgentDir, w.Config.Runner.Path = binary, agentDir, os.Getenv("PATH")
			t.Logf("POLICY: %s; MODEL: %s/%s", PolicyVersion, w.Config.Runner.Provider, w.Config.Runner.Model)
			t.Logf("REPORT: %s\nWANT: %s\nHUMAN REVIEW: %s", tc.Report, tc.Want, tc.Review)
			if tc.Example != "" {
				t.Logf("ILLUSTRATIVE HEADLINE (not an exact-match answer): %s", tc.Example)
			}
			out, err := (PiRunner{Config: w.Config.Runner}).Run(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("ACTUAL: %s", b)
			if err := checkPersonalityOutput(tc, in, out); err != nil {
				t.Fatal(err)
			}
			verdict, err := runPersonalityJudge(t, w.Config.Runner, in, out)
			if err != nil {
				t.Fatalf("judge error (not a quality verdict): %v", err)
			}
			b, err = json.Marshal(verdict)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("JUDGE (fallible assessment): %s", b)
			for name, grade := range map[string]personalityGrade{
				"fidelity": verdict.Fidelity, "disposition": verdict.Disposition,
				"simplicity": verdict.Simplicity, "voice": verdict.Voice,
			} {
				if !*grade.Pass {
					t.Errorf("judge failed %s: %s (quote: %q)", name, grade.Reason, grade.Quote)
				}
			}
		})
	}
}
