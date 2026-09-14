package athena

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
)

// Exercises editorial choices with synthetic reports only. Normal tests cover
// deterministic linkage/protection; live model quality remains an opt-in check.
func TestOptInEditorialConsolidation(t *testing.T) {
	if os.Getenv("DAYLOG_LUNA_SMOKE") != "1" {
		t.Skip("synthetic model evaluation is opt-in")
	}
	binary, agentDir := os.Getenv("DAYLOG_SMOKE_PI"), os.Getenv("DAYLOG_SMOKE_PI_AGENT_DIR")
	if binary == "" || agentDir == "" {
		t.Fatal("set DAYLOG_SMOKE_PI and DAYLOG_SMOKE_PI_AGENT_DIR")
	}
	b, err := os.ReadFile("../../testdata/gatekeeper/editorial.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name                  string   `json:"name"`
		Existing              string   `json:"existing"`
		ExistingDetails       string   `json:"existing_details"`
		Reports               []string `json:"reports"`
		Want                  string   `json:"want"`
		PreserveProcessingGap bool     `json:"preserve_processing_gap"`
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			w, _, base := fixture(t, "live")
			w.Config.Runner.Binary, w.Config.Runner.AgentDir, w.Config.Runner.Path = binary, agentDir, os.Getenv("PATH")
			base.Refs = []string{"gh:pr:github.com/o/r#142"}
			var group []capture.Item
			for i, text := range tc.Reports {
				c := base
				c.ID, c.Text, c.Terminal = fmt.Sprintf("report-%d", i), text, "unknown"
				c.OccurredAt = w.now().Add(time.Duration(i-10) * time.Minute).Format(time.RFC3339)
				group = append(group, capture.Item{Candidate: c})
			}
			in, err := w.input(group)
			if err != nil {
				t.Fatal(err)
			}
			if tc.Existing != "" {
				in.Outcomes = []event.Entry{{Event: event.Event{ID: event.NewID(w.now()), Source: "agent:pi", Type: "work", TLDR: tc.Existing, Details: tc.ExistingDetails, Refs: base.Refs, Context: base.Context, Provenance: &event.Provenance{Episode: "a-different-agent-turn"}}, Revision: 1, DisplayAt: base.OccurredAt}}
				in.EditableTargets = editableTargets(in)
			}
			out, err := (PiRunner{Config: w.Config.Runner}).Run(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(out)
			t.Log(string(b)) // Inspect nuance, including malformed responses.
			if err := Validate(in, out); err != nil {
				t.Fatal(err)
			}
			if len(out.Actions) != 1 {
				t.Fatalf("expected one outcome/disposition, got %d", len(out.Actions))
			}
			a := out.Actions[0]
			if tc.Want == "update-or-skip" {
				if a.Kind != "amend" && a.Kind != "skip" {
					t.Errorf("verification update must not create another row: %s", a.Kind)
				}
			} else if a.Kind != tc.Want {
				t.Errorf("want %s, got %s", tc.Want, a.Kind)
			}
			if tc.Name == "focus-followup" {
				text := strings.ToLower(a.Text + " " + a.Details)
				if !strings.Contains(text, "escape") || !strings.Contains(text, "focus") {
					t.Error("amendment lost the earlier keyboard outcome", text)
				}
			}
			if tc.PreserveProcessingGap && a.Kind == "amend" {
				text := strings.ToLower(a.Text + " " + a.Details)
				if !strings.Contains(text, "processing") || (!strings.Contains(text, "untested") && !strings.Contains(text, "unverified") && !strings.Contains(text, "not") && !strings.Contains(text, "unvalidated")) {
					t.Error("inspect lost later-change verification gap", text)
				}
			}
		})
	}
}
