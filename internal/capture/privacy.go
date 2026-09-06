package capture

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var sensitiveLine = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|authorization|\.env|BEGIN .*PRIVATE KEY|sk-[a-zA-Z0-9]|gh[pousr]_[a-zA-Z0-9])`)

// Redact excludes obvious sensitive lines and bounds excerpts. It is not a
// security guarantee and does not replace explicit per-project cloud approval.
func Redact(text string, maxBytes int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if sensitiveLine.MatchString(line) {
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
