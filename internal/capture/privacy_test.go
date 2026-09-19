package capture

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const excludedLine = "[sensitive line excluded]"

func TestRedactPreservesOrdinaryHyphenation(t *testing.T) {
	for _, text := range []string{
		"Implemented mask-edit and verified the desk-layout fix.",
		"Completed task-based grouping; disk-backed storage is unchanged.",
		"risk-aware checks preserve multi-line and well-known wording.",
		"MASK-EDIT works with TASK-BASED grouping.",
		"Updated the mask-editor, then checked mask-edit again.",
		"A prefix inside an identifier is not a token: widget_ghp_example.",
		"Unicode identifiers: αsk-example and 界sk-example and e\u0301sk-example.",
		"Bare format names: sk- and ghp_ and gho_ and ghu_ and ghs_ and ghr_.",
	} {
		t.Run(text, func(t *testing.T) {
			if got := Redact(text, MaxReportBytes); got != text {
				t.Fatalf("Redact() = %q, want unchanged %q", got, text)
			}
		})
	}
}

func TestRedactSupportedCredentialPrefixes(t *testing.T) {
	// Hand-authored synthetic bodies, not credentials. Keep short/truncated
	// forms covered too: redaction must not depend on a provider's exact length.
	bodies := []string{"A", strings.Repeat("aB12", 9)}
	prefixes := []string{"sk-", "sk-proj-", "sk-svcacct-", "sk-ant-api03-", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "SK-", "GHP_"}
	wrappers := []struct{ before, after string }{
		{"", ""},
		{" ", " "},
		{"value=", "; done"},
		{"\"", "\","},
		{"'", "'"},
		{"`", "`"},
		{"(", ")."},
		{"[", "],"},
		{"{", "}"},
		{"prefix/", "/suffix"},
		{"prefix-", "-suffix"},
		{"before:\t", "\tafter"},
		{"before — ", "…"},
	}
	for _, prefix := range prefixes {
		for _, body := range bodies {
			for _, wrapper := range wrappers {
				text := wrapper.before + prefix + body + wrapper.after
				if got := Redact(text, MaxReportBytes); got != excludedLine {
					t.Errorf("Redact(%q) = %q, want excluded line", text, got)
				}
			}
		}
	}
}

func TestRedactSensitiveLabelsRemainProtected(t *testing.T) {
	for _, text := range []string{
		"API_KEY=synthetic", "api-key: synthetic", "apikey=synthetic",
		"access_token=synthetic", "access-token: synthetic", "accesstoken=synthetic",
		"refresh_token=synthetic", "refresh-token: synthetic", "refreshtoken=synthetic",
		"password=synthetic", "secret=synthetic", "Authorization: Bearer synthetic",
		"Read .env.local", "-----BEGIN RSA PRIVATE KEY-----",
	} {
		if got := Redact(text, MaxReportBytes); got != excludedLine {
			t.Errorf("Redact(%q) = %q, want excluded line", text, got)
		}
	}
}

func TestRedactMultilineAndMixedText(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"empty", " \t\r\n ", ""},
		{"normal", "mask-edit works\r\n\r\ndisk-backed checks passed", "mask-edit works\n\ndisk-backed checks passed"},
		{"mixed", "mask-edit works\r\nvalue=sk-proj-synthetic_aB12\r\n\r\ndisk-backed checks passed", "mask-edit works\n" + excludedLine + "\n\ndisk-backed checks passed"},
		{"same line", "mask-edit works; value=ghp_synthetic; checks passed", excludedLine},
		{"fully excluded", "sk-synthetic\n\nghp_synthetic", excludedLine + "\n\n" + excludedLine},
		{"multiple", "gho_synthetic, ghu_synthetic\nghs_synthetic; ghr_synthetic", excludedLine + "\n" + excludedLine},
		{"marker retained", excludedLine, excludedLine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.text, MaxReportBytes)
			if got != tc.want {
				t.Fatalf("Redact() = %q, want %q", got, tc.want)
			}
			if again := Redact(got, MaxReportBytes); again != got {
				t.Fatalf("redaction is not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestRedactBoundsAfterFiltering(t *testing.T) {
	const truncation = "\n[excerpt truncated]"
	text := "mask-edit 界界界\nvalue=sk-synthetic\n" + strings.Repeat("safe ", 50)
	for limit := 0; limit <= len(text); limit++ {
		got := Redact(text, limit)
		if len(got) > limit || !utf8.ValidString(got) {
			t.Fatalf("limit %d: invalid excerpt %q (%d bytes)", limit, got, len(got))
		}
		if strings.Contains(got, "sk-synthetic") {
			t.Fatalf("limit %d: synthetic credential survived", limit)
		}
	}
	// Slice inside a multibyte rune; preserve the complete preceding text.
	if got := Redact(text, len("mask-edit 界")+1+len(truncation)); got != "mask-edit 界"+truncation {
		t.Fatalf("UTF-8 truncation = %q", got)
	}
	if got := Redact("sk-"+strings.Repeat("a", 100), len(excludedLine)); got != excludedLine {
		t.Fatalf("filtering must precede truncation: %q", got)
	}
}
