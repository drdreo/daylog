// Package event defines the single supported ledger and effective-entry contract.
package event

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const Version = 2
const MaxTLDRChars = 280 // Existing entries and human notes retain their original limit.
const MaxHeadlineChars = 100
const MaxDetailsChars = 2000
const MaxTags = 3
const MaxTagChars = 24
const (
	TypeWork        = "work"
	TypeSidequest   = "sidequest"
	TypeNote        = "note"
	TypeTodo        = "todo"
	TypeDone        = "done"
	TypeReopen      = "reopen"
	TypeTriage      = "triage"
	TypeAmend       = "amend"
	TypeDismiss     = "dismiss"
	TypeRestore     = "restore"
	TypeMerge       = "merge"
	VerdictAccepted = "accepted"
	VerdictDeclined = "declined"
)

var AddableTypes = []string{TypeWork, TypeSidequest, TypeNote, TypeTodo}

type Repository struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

func (r Repository) Key() string {
	if r.Host == "" || r.Path == "" {
		return ""
	}
	return r.Host + "/" + r.Path
}

type Context struct {
	Cwd           string     `json:"cwd"`
	Worktree      string     `json:"worktree,omitempty"`
	Branch        string     `json:"branch,omitempty"`
	Head          string     `json:"head,omitempty"`
	Repository    Repository `json:"repository"`
	Session       string     `json:"session,omitempty"`
	Turn          string     `json:"turn,omitempty"`
	Task          string     `json:"task,omitempty"`
	ParentSession string     `json:"parent_session,omitempty"`
}

// SameProject never treats two missing repository identities as a match.
func SameDay(a, b string) bool {
	at, ae := time.Parse(time.RFC3339Nano, a)
	bt, be := time.Parse(time.RFC3339Nano, b)
	return ae == nil && be == nil && at.Format("2006-01-02") == bt.Format("2006-01-02")
}
func SameProject(a, b Context) bool {
	if a.Repository.Key() != "" && b.Repository.Key() != "" {
		return a.Repository == b.Repository
	}
	ap := a.Worktree
	if ap == "" {
		ap = a.Cwd
	}
	bp := b.Worktree
	if bp == "" {
		bp = b.Cwd
	}
	return ap != "" && ap == bp
}

type Provenance struct {
	Candidates []string `json:"candidates"`
	Evidence   []string `json:"evidence"`
	Sources    []string `json:"sources"`
	Model      string   `json:"model"`
	Policy     string   `json:"policy"`
	Episode    string   `json:"episode"`
}
type Target struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}
type Event struct {
	Version        int         `json:"version"`
	Sequence       int         `json:"sequence"`
	ID             string      `json:"id"`
	PublicationKey string      `json:"publication_key"`
	RecordedAt     string      `json:"recorded_at"`
	OccurredAt     string      `json:"occurred_at"`
	TimeBasis      string      `json:"time_basis"`
	Host           string      `json:"host"`
	Source         string      `json:"source"`
	Type           string      `json:"type"`
	TLDR           string      `json:"tldr,omitempty"`
	Details        string      `json:"details,omitempty"`
	Tags           []string    `json:"tags,omitempty"`
	Refs           []string    `json:"refs"`
	Context        Context     `json:"context"`
	Targets        []Target    `json:"targets,omitempty"`
	ToType         string      `json:"to_type,omitempty"`
	Verdict        string      `json:"verdict,omitempty"`
	Reason         string      `json:"reason,omitempty"`
	Provenance     *Provenance `json:"provenance,omitempty"`
}
type Entry struct {
	Event
	Revision     int      `json:"revision"`
	DisplayAt    string   `json:"display_at"`
	FiledAt      string   `json:"filed_at"`
	Done         bool     `json:"done"`
	DoneNote     string   `json:"done_note,omitempty"`
	Pinned       bool     `json:"pinned"`
	Dismissed    bool     `json:"dismissed"`
	MergedInto   string   `json:"merged_into,omitempty"`
	Contributors []string `json:"contributors"`
}

func NewID(t time.Time) string { return ulid.MustNew(ulid.Timestamp(t), rand.Reader).String() }
func ShortID(id string) string {
	if len(id) > 12 {
		id = id[:12]
	}
	return strings.ToLower(id)
}

var sourceRe = regexp.MustCompile(`^(agent|human):[a-z0-9][a-z0-9_.-]*$`)

func ValidateSource(s string) error {
	if !sourceRe.MatchString(s) {
		return fmt.Errorf("invalid source %q: expected agent:<name> or human:<name>", s)
	}
	return nil
}
func Human(s string) bool     { return strings.HasPrefix(s, "human:") }
func Narrative(t string) bool { return t == TypeWork || t == TypeSidequest || t == TypeNote }
func ValidateTLDR(s string) error {
	if strings.TrimSpace(s) == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxTLDRChars || strings.ContainsAny(s, "\r\n\x00") {
		return fmt.Errorf("tldr must be a nonempty single line of at most %d characters", MaxTLDRChars)
	}
	return nil
}

// Presentation is plain text plus small label chips, never executable markup.
func ValidatePresentation(details string, tags []string) error {
	if !utf8.ValidString(details) || utf8.RuneCountInString(details) > MaxDetailsChars || strings.ContainsAny(details, "\r\x00") {
		return fmt.Errorf("details must be plain text of at most %d characters", MaxDetailsChars)
	}
	if len(tags) > MaxTags {
		return fmt.Errorf("at most %d tags allowed", MaxTags)
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		key := strings.ToLower(tag)
		if tag == "" || tag != strings.TrimSpace(tag) || !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > MaxTagChars || strings.ContainsAny(tag, "\r\n\t\x00") || seen[key] {
			return fmt.Errorf("tags must be unique single-line labels of at most %d characters", MaxTagChars)
		}
		seen[key] = true
	}
	return nil
}
func ValidateAddType(t string) error {
	if Narrative(t) || t == TypeTodo {
		return nil
	}
	return fmt.Errorf("invalid report type %q", t)
}
func ValidateVerdict(v string) error {
	if v != VerdictAccepted && v != VerdictDeclined {
		return fmt.Errorf("invalid verdict %q", v)
	}
	return nil
}

var typedRef = regexp.MustCompile(`^(gh:(pr|issue):[a-zA-Z0-9.-]+/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+#[1-9][0-9]*|(linear|jira):[A-Z][A-Z0-9]*-[1-9][0-9]*)$`)
var issueURLPath = regexp.MustCompile(`^/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)/issues/([1-9][0-9]*)/?$`)
var shortRef = regexp.MustCompile(`^#[1-9][0-9]*$`)
var trackerRef = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[1-9][0-9]*$`)

func NormalizeRef(r string, repo Repository) (string, error) {
	r = strings.TrimSpace(r)
	// Issue URLs are intake shorthand, never PRs. Keep only the issue identity,
	// discarding query/fragment navigation. Host-qualified refs also support GHES.
	if u, err := url.Parse(r); err == nil && u.Scheme == "https" && u.User == nil && u.Host != "" && u.Host == u.Hostname() {
		if m := issueURLPath.FindStringSubmatch(u.EscapedPath()); m != nil && m[1] != "." && m[1] != ".." && m[2] != "." && m[2] != ".." {
			r = "gh:issue:" + strings.ToLower(u.Host) + "/" + m[1] + "/" + m[2] + "#" + m[3]
		}
	}
	if shortRef.MatchString(r) && repo.Key() != "" {
		r = "gh:pr:" + repo.Key() + r
	}
	if trackerRef.MatchString(r) {
		r = "linear:" + r
	}
	if !typedRef.MatchString(r) {
		return "", fmt.Errorf("invalid ref %q: use gh:issue:host/owner/repo#N, an HTTPS GitHub issue URL, gh:pr:host/owner/repo#N, linear:ID-N, jira:ID-N, or #N for a PR in a repository", r)
	}
	return r, nil
}
func (e Event) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("unsupported event version %d (want %d)", e.Version, Version)
	}
	if _, err := ulid.ParseStrict(e.ID); err != nil {
		return fmt.Errorf("invalid event id: %w", err)
	}
	if e.PublicationKey == "" || len(e.PublicationKey) > 256 {
		return fmt.Errorf("publication key required and bounded")
	}
	if err := ValidateSource(e.Source); err != nil {
		return err
	}
	for _, ts := range []string{e.RecordedAt, e.OccurredAt} {
		if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
			return fmt.Errorf("invalid required timestamp: %w", err)
		}
	}
	if e.TimeBasis != "report" && e.TimeBasis != "native" && e.TimeBasis != "human" {
		return fmt.Errorf("invalid time basis")
	}
	if len(e.Refs) > 32 || len(e.Targets) > 16 || len(e.Reason) > 1024 || len(e.TLDR) > 4096 {
		return fmt.Errorf("event exceeds bounds")
	}
	if err := ValidatePresentation(e.Details, e.Tags); err != nil {
		return err
	}
	if (e.Details != "" || len(e.Tags) != 0) && !Narrative(e.Type) && e.Type != TypeAmend && e.Type != TypeMerge {
		return fmt.Errorf("details and tags belong only to narrative entries")
	}
	for _, r := range e.Refs {
		if _, err := NormalizeRef(r, Repository{}); err != nil {
			return err
		}
	}
	switch e.Type {
	case TypeWork, TypeSidequest, TypeNote, TypeTodo:
		if len(e.Targets) != 0 {
			return fmt.Errorf("new entry cannot target existing entries")
		}
		if err := ValidateTLDR(e.TLDR); err != nil {
			return err
		}
	case TypeAmend, TypeMerge:
		if err := ValidateTLDR(e.TLDR); err != nil {
			return err
		}
		if e.ToType != "" && !Narrative(e.ToType) {
			return fmt.Errorf("amend cannot create obligations")
		}
		fallthrough
	case TypeDone, TypeReopen, TypeTriage, TypeDismiss, TypeRestore:
		n := 1
		if e.Type == TypeMerge {
			n = 2
		}
		if len(e.Targets) < n || (e.Type != TypeMerge && len(e.Targets) != 1) {
			return fmt.Errorf("invalid target count")
		}
		seen := map[string]bool{}
		for _, t := range e.Targets {
			if t.ID == "" || t.Revision < 1 || seen[t.ID] {
				return fmt.Errorf("invalid or duplicate target")
			}
			seen[t.ID] = true
		}
		if (e.Type == TypeDone || e.Type == TypeReopen || e.Type == TypeTriage || e.Type == TypeDismiss || e.Type == TypeRestore) && !Human(e.Source) {
			return fmt.Errorf("operation requires human source")
		}
		if e.Type == TypeTriage {
			if err := ValidateVerdict(e.Verdict); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported event type %q", e.Type)
	}
	if !Human(e.Source) && Narrative(e.Type) && e.Provenance == nil {
		return fmt.Errorf("agent narrative requires editorial provenance")
	}
	return nil
}

// Effective applies events in ledger order, including suppressed identities.
func Effective(all []Event) map[string]Entry {
	out := map[string]Entry{}
	for _, e := range all {
		if len(e.Targets) == 0 {
			out[e.ID] = Entry{Event: e, Revision: 1, DisplayAt: e.OccurredAt, FiledAt: e.OccurredAt, Contributors: []string{e.ID}}
			continue
		}
		for i, t := range e.Targets {
			en, ok := out[t.ID]
			if !ok {
				continue
			}
			en.Revision++
			switch e.Type {
			case TypeAmend:
				en.Refs = unique(append(append([]string{}, en.Refs...), e.Refs...))
				en.TLDR = e.TLDR
				en.Details, en.Tags = e.Details, e.Tags
				if e.ToType != "" {
					en.Type = e.ToType
				}
				en.Pinned = en.Pinned || Human(e.Source)
			case TypeDismiss:
				en.Dismissed = true
			case TypeRestore:
				en.Dismissed = false
			case TypeDone:
				en.Done = true
				en.DisplayAt = e.OccurredAt
				en.DoneNote = e.TLDR
			case TypeReopen:
				en.Done = false
				en.DisplayAt = en.FiledAt
				en.DoneNote = ""
			case TypeTriage:
				en.Verdict = e.Verdict
			case TypeMerge:
				if i == 0 {
					en.TLDR = e.TLDR
					en.Details, en.Tags = e.Details, e.Tags
					en.Refs = unique(append(append([]string{}, en.Refs...), e.Refs...))
					en.Pinned = en.Pinned || Human(e.Source)
					for _, other := range e.Targets[1:] {
						en.Provenance = combineProvenance(en.Provenance, out[other.ID].Provenance)
						en.Refs = unique(append(append([]string{}, en.Refs...), out[other.ID].Refs...))
						en.Contributors = append(en.Contributors, out[other.ID].Contributors...)
					}
				} else {
					en.MergedInto = e.Targets[0].ID
				}
			}
			if e.Provenance != nil {
				en.Provenance = combineProvenance(en.Provenance, e.Provenance)
			}
			out[t.ID] = en
		}
	}
	return out
}
func combineProvenance(a, b *Provenance) *Provenance {
	if b == nil {
		return a
	}
	if a == nil {
		return b
	}
	c := *b
	c.Candidates = unique(append(append([]string{}, a.Candidates...), b.Candidates...))
	c.Evidence = unique(append(append([]string{}, a.Evidence...), b.Evidence...))
	c.Sources = unique(append(append([]string{}, a.Sources...), b.Sources...))
	return &c
}
func unique(xs []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// CheckTargets must run under the store lock, immediately before append.
func CheckTargets(e Event, current map[string]Entry) error {
	for _, t := range e.Targets {
		en, ok := current[t.ID]
		if !ok || en.Revision != t.Revision {
			return fmt.Errorf("stale or unknown target %s revision %d", t.ID, t.Revision)
		}
		if en.MergedInto != "" {
			return fmt.Errorf("target merged into %s", en.MergedInto)
		}
		if !Human(e.Source) && (Human(en.Source) || en.Pinned || en.Dismissed || en.Type == TypeTodo) {
			return fmt.Errorf("target %s is human-protected", t.ID)
		}
		if (e.Type == TypeDone || e.Type == TypeReopen || e.Type == TypeTriage) && en.Type != TypeTodo {
			return fmt.Errorf("only todos have lifecycle operations")
		}
		if e.Type == TypeDone && (en.Done || en.Verdict == VerdictDeclined || (!Human(en.Source) && en.Verdict != VerdictAccepted)) {
			return fmt.Errorf("todo must be adopted and open before completion")
		}
		if e.Type == TypeReopen && !en.Done {
			return fmt.Errorf("todo must be completed before reopening")
		}
		if (e.Type == TypeAmend || e.Type == TypeMerge || e.Type == TypeDismiss || e.Type == TypeRestore) && !Narrative(en.Type) {
			return fmt.Errorf("narrative correction requires narrative targets")
		}
	}
	if e.Type == TypeMerge {
		base := current[e.Targets[0].ID]
		for _, t := range e.Targets[1:] {
			other := current[t.ID]
			if !SameProject(base.Context, other.Context) || !SameDay(base.DisplayAt, other.DisplayAt) {
				return fmt.Errorf("merge requires same repository and occurrence day")
			}
		}
	}
	return nil
}
