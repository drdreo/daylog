package event

import (
	"strings"
	"testing"
	"time"
)

func TestNewIDSortable(t *testing.T) {
	a := NewID(time.Now())
	b := NewID(time.Now().Add(time.Second))
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("expected 26-char ULIDs, got %q %q", a, b)
	}
	if a >= b {
		t.Fatalf("ULIDs not time-sortable: %q >= %q", a, b)
	}
}

func TestValidateSource(t *testing.T) {
	for _, ok := range []string{"agent:claude", "human:cli", "human:slack", "poller:gh", "agent:pi"} {
		if err := ValidateSource(ok); err != nil {
			t.Errorf("ValidateSource(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "claude", "Agent:claude", "agent:", "bot:x", "agent:Claude", "agent claude"} {
		if err := ValidateSource(bad); err == nil {
			t.Errorf("ValidateSource(%q) = nil, want error", bad)
		}
	}
}

func TestValidateTLDR(t *testing.T) {
	if err := ValidateTLDR("fixed the thing"); err != nil {
		t.Errorf("valid tldr rejected: %v", err)
	}
	if err := ValidateTLDR(strings.Repeat("ä", 280)); err != nil {
		t.Errorf("280 runes should pass (rune count, not bytes): %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 281), "line\nbreak"} {
		if err := ValidateTLDR(bad); err == nil {
			t.Errorf("ValidateTLDR(%q) = nil, want error", bad)
		}
	}
}

func TestNormalizeRef(t *testing.T) {
	cases := []struct {
		ref, repo, want string
		wantErr         bool
	}{
		{"#142", "owner/repo", "gh:pr:owner/repo#142", false},
		{"#142", "", "", true}, // shorthand outside a repo context
		{"ABC-123", "", "linear:ABC-123", false},
		{"gh:pr:owner/repo#7", "", "gh:pr:owner/repo#7", false},
		{"jira:PROJ-45", "", "jira:PROJ-45", false},
		{"slack:C0123/p1692", "", "slack:C0123/p1692", false},
		{"newscheme:whatever", "", "newscheme:whatever", false}, // unknown schemes pass through
		{"just words", "", "", true},
		{"", "owner/repo", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeRef(c.ref, c.repo)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeRef(%q, %q) = %q, want error", c.ref, c.repo, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeRef(%q, %q) = %q, %v; want %q", c.ref, c.repo, got, err, c.want)
		}
	}
}
