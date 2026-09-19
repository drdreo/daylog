package capture

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var sensitiveLine = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|authorization|\.env|BEGIN .*PRIVATE KEY)`)

// Credential prefixes must start a token, not occur inside words such as
// "mask-edit". Include Unicode letters, numbers and combining marks in the
// identifier boundary; regexp's ASCII-only \b would split Unicode words.
// Allow emphasis underscores after that outer boundary, so Markdown cannot
// hide credentials while prefixes inside widget_ghp_example remain untouched.
// Keep detection conservative after that boundary: even short/truncated keys
// are excluded, without provider-specific lengths or a trailing boundary that
// could miss longer keys and hyphen/underscore-bearing variants.
var sensitiveCredential = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}\p{M}_])_*(sk-[a-z0-9]|gh[pousr]_[a-z0-9])`)

// Redact excludes obvious sensitive lines and bounds excerpts. It is not a
// security guarantee: queued reports and evidence may be sent to the configured model.
func Redact(text string, maxBytes int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if sensitiveLine.MatchString(line) || sensitiveCredential.MatchString(line) {
			lines[i] = "[sensitive line excluded]"
		}
	}
	text = strings.Join(lines, "\n")
	if len(text) > maxBytes {
		const marker = "\n[excerpt truncated]"
		n := maxBytes - len(marker)
		if n < 0 {
			return ""
		}
		text = text[:n]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += marker
	}
	return strings.TrimSpace(text)
}
