package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/view"
)

func TestAddGitHubIssueReferences(t *testing.T) {
	for _, source := range []string{"agent:pi", "human:cli"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			want := []string{"gh:issue:github.com/drdreo/Athena#22", "gh:pr:github.com/drdreo/Athena#22"}
			out, err := runCLI(t, home, source, "add", "--type", "work",
				"--ref", "https://github.com/drdreo/Athena/issues/22#issuecomment-1", "--ref", want[1], "Synthetic issue reference regression check.")
			if err != nil {
				t.Fatal(out, err)
			}
			if source == "agent:pi" {
				if !strings.HasPrefix(out, "queued ") {
					t.Fatal(out)
				}
				out, err = runCLI(t, home, source, "queue", "list")
				var items []capture.Item
				if err != nil || json.Unmarshal([]byte(out), &items) != nil || len(items) != 1 {
					t.Fatal(out, err)
				}
				if !reflect.DeepEqual(items[0].Candidate.Refs, want) {
					t.Fatal(items[0].Candidate.Refs)
				}
				if err := items[0].Candidate.Validate(); err != nil {
					t.Fatal(err)
				}
			}
			out, err = runCLI(t, home, source, "today", "--json")
			var day view.Day
			if err != nil || json.Unmarshal([]byte(out), &day) != nil {
				t.Fatal(out, err)
			}
			if source == "agent:pi" {
				if len(day.Entries) != 0 {
					t.Fatal("capture published directly", day.Entries)
				}
			} else if len(day.Entries) != 1 || !reflect.DeepEqual(day.Entries[0].Refs, want) {
				t.Fatal(day.Entries)
			}
			if len(day.PRs) != 0 {
				t.Fatal("issue reference created PR status", day.PRs)
			}
		})
	}
}

func TestRejectedIssueURLDoesNotEnqueue(t *testing.T) {
	home := t.TempDir()
	out, err := runCLI(t, home, "agent:pi", "add", "--type", "work", "--ref",
		"https://user:secret@github.com/drdreo/Athena/issues/22", "Synthetic invalid issue reference.")
	if err == nil || !strings.Contains(out, "invalid ref") {
		t.Fatal(out, err)
	}
	out, err = runCLI(t, home, "agent:pi", "queue", "list")
	var items []capture.Item
	if err != nil || json.Unmarshal([]byte(out), &items) != nil || len(items) != 0 {
		t.Fatal(out, err)
	}
}
