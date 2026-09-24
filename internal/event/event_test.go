package event

import (
	"strings"
	"testing"
	"time"
)

func TestContract(t *testing.T) {
	for _, s := range []string{"human:cli", "agent:pi", "agent:codex", "agent:claude"} {
		if err := ValidateSource(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"poller:gh", "Agent:x", "", "agent:"} {
		if ValidateSource(s) == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", strings.Repeat("a", 281), "a\nb"} {
		if ValidateTLDR(s) == nil {
			t.Fatal(s)
		}
	}
	if ValidateTLDR(strings.Repeat("ä", 280)) != nil {
		t.Fatal("rune cap")
	}
	r, e := NormalizeRef("#142", Repository{"github.example", "owner/repo"})
	if e != nil || r != "gh:pr:github.example/owner/repo#142" {
		t.Fatal(r, e)
	}
	for _, r := range []string{"gh:pr:owner/repo#1", "https:evil", "#0"} {
		if _, e := NormalizeRef(r, Repository{}); e == nil {
			t.Fatal(r)
		}
	}
}
func TestNormalizeGitHubIssueRefs(t *testing.T) {
	repo := Repository{"github.example", "other/repo"}
	for _, tc := range []struct{ input, want string }{
		{"gh:issue:github.com/drdreo/Athena#22", "gh:issue:github.com/drdreo/Athena#22"},
		{"https://github.com/drdreo/Athena/issues/22", "gh:issue:github.com/drdreo/Athena#22"},
		{" https://GITHUB.COM/drdreo/Athena/issues/22/?q=hello#issuecomment-123 ", "gh:issue:github.com/drdreo/Athena#22"},
		{"https://github.example/team/.github/issues/9", "gh:issue:github.example/team/.github#9"},
		{"gh:pr:github.com/drdreo/Athena#22", "gh:pr:github.com/drdreo/Athena#22"},
		{"#22", "gh:pr:github.example/other/repo#22"},
		{"ABC-12", "linear:ABC-12"},
		{"jira:ABC-12", "jira:ABC-12"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeRef(tc.input, repo)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			if again, err := NormalizeRef(got, Repository{}); err != nil || again != got {
				t.Fatalf("canonical ref did not round trip: %q, %v", again, err)
			}
		})
	}
	for _, input := range []string{
		"gh:issue:owner/repo#22", "gh:issue:github.com/o/r#0",
		"gh:issue:github.com@evil.example/o/r#22", "gh:issue:github.com/o/r#-1",
		"http://github.com/o/r/issues/22", "javascript://github.com/o/r/issues/22",
		"//github.com/o/r/issues/22", "https://user:secret@github.com/o/r/issues/22",
		"https://github.com@evil.example/o/r/issues/22", "https://github.com:443/o/r/issues/22",
		"https://github.com/o/r/issues/0", "https://github.com/o/r/issues/01",
		"https://github.com/o/r/issues/-1", "https://github.com/o/r/issues/22/extra",
		"https://github.com/o/r/pull/22", "https://github.com/o/r/issues",
		"https://github.com/o%2Fr/r/issues/22", "https://github.com/o/r/issues/%32%32",
		"https://github.com/../r/issues/22", "https://github.com/o/./issues/22",
		"https://github.com/o/r/issues/22\nhttps://evil.example",
		"https://github.com/o/r/issues/22#%ZZ",
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := NormalizeRef(input, repo); err == nil {
				t.Fatalf("accepted %q as %q", input, got)
			}
		})
	}
}

func TestHumanProtection(t *testing.T) {
	now := time.Now()
	base := Event{ID: NewID(now), Type: TypeWork, TLDR: "original", Source: "agent:pi", OccurredAt: now.Format(time.RFC3339)}
	amend := Event{Type: TypeAmend, Source: "human:cli", TLDR: "human wording", Targets: []Target{{base.ID, 1}}}
	current := Effective([]Event{base, amend})
	e := Event{Type: TypeAmend, Source: "agent:pi", Targets: []Target{{base.ID, 2}}}
	if CheckTargets(e, current) == nil {
		t.Fatal("automated edit bypassed pin")
	}
	if current[base.ID].TLDR != "human wording" {
		t.Fatal(current)
	}
}
