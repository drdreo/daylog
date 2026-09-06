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
