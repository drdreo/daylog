// Package event defines the daylog event schema and write-time validation.
// The CLI is the single write path (ARCHITECTURE.md §2): every rule that
// keeps the store consistent lives here, not in producers.
package event

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

// MaxTLDRChars is enforced at write time; oversized entries are rejected,
// never truncated (§5).
const MaxTLDRChars = 280

// Core event types (§4.2). Consumers must tolerate unknown types.
const (
	TypeWork       = "work"
	TypeSidequest  = "sidequest"
	TypeNote       = "note"
	TypeTodo       = "todo"
	TypeDone       = "done"
	TypeReclassify = "reclassify"
	TypeTransition = "transition"
)

// AddableTypes are the types a producer may pass to `daylog add`.
var AddableTypes = []string{TypeWork, TypeSidequest, TypeNote, TypeTodo}

// Ctx is auto-captured by the CLI on add, never supplied by the caller (§4.1).
type Ctx struct {
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
}

// Event is one line in the day's JSONL file. Immutable once written.
type Event struct {
	ID     string         `json:"id"`
	TS     string         `json:"ts"`
	Host   string         `json:"host"`
	Source string         `json:"source"`
	Type   string         `json:"type"`
	TLDR   string         `json:"tldr"`
	Refs   []string       `json:"refs"`
	Ctx    Ctx            `json:"ctx"`
	Parent *string        `json:"parent"`
	Meta   map[string]any `json:"meta"`
}

// NewID returns a ULID for the given time: lexically sortable, globally
// unique without coordination (§4.1).
func NewID(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), rand.Reader).String()
}

// ShortID is the display form of a ULID. A ULID's first 10 chars encode
// the millisecond timestamp, so entries logged in the same second share an
// 8-char prefix; 12 chars include entropy and stay pasteable.
func ShortID(id string) string {
	if len(id) > 12 {
		id = id[:12]
	}
	return strings.ToLower(id)
}

var sourceRe = regexp.MustCompile(`^(agent|human|poller):[a-z0-9][a-z0-9_.-]*$`)

// ValidateSource checks the namespaced producer identity (§4.1).
func ValidateSource(s string) error {
	if !sourceRe.MatchString(s) {
		return fmt.Errorf("invalid source %q: must be agent:<name>, human:<name>, or poller:<name> (lowercase)", s)
	}
	return nil
}

// ValidateTLDR enforces the non-empty, ≤280-char rule (§5).
func ValidateTLDR(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("tldr must not be empty")
	}
	if strings.ContainsAny(s, "\n\r") {
		return fmt.Errorf("tldr must be a single line")
	}
	if n := utf8.RuneCountInString(s); n > MaxTLDRChars {
		return fmt.Errorf("tldr is %d chars, limit is %d: rewrite it shorter (entries are rejected, not truncated)", n, MaxTLDRChars)
	}
	return nil
}

// ValidateAddType restricts `daylog add` to the producer vocabulary.
func ValidateAddType(t string) error {
	for _, ok := range AddableTypes {
		if t == ok {
			return nil
		}
	}
	return fmt.Errorf("invalid type %q: must be one of %s", t, strings.Join(AddableTypes, "|"))
}

var (
	prShorthandRe = regexp.MustCompile(`^#(\d+)$`)
	schemeRe      = regexp.MustCompile(`^[a-z][a-z0-9]*:`)
	trackerIDRe   = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)
)

// NormalizeRef turns caller shorthand into a typed URI (§4.3).
//   - "#142" inside a repo context → "gh:pr:owner/repo#142"
//   - "ABC-123" → "linear:ABC-123" (Linear is the only tracker in use;
//     Jira callers must pass an explicit "jira:" ref)
//   - anything already scheme-qualified passes through untouched, so new
//     producers can introduce ref schemes before consumers learn them.
func NormalizeRef(ref, ctxRepo string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty ref")
	}
	if m := prShorthandRe.FindStringSubmatch(ref); m != nil {
		if ctxRepo == "" {
			return "", fmt.Errorf("ref %q needs a repo context: run inside a git repo or use the full form gh:pr:owner/repo#%s", ref, m[1])
		}
		return fmt.Sprintf("gh:pr:%s#%s", ctxRepo, m[1]), nil
	}
	if trackerIDRe.MatchString(ref) {
		return "linear:" + ref, nil
	}
	if schemeRe.MatchString(ref) {
		return ref, nil
	}
	return "", fmt.Errorf("invalid ref %q: use scheme-qualified form (gh:pr:owner/repo#142, linear:ABC-123, jira:PROJ-45) or #N inside a repo", ref)
}
