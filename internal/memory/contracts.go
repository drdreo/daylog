// Package memory stores explicitly supplied owner-labelled records. It does not
// ingest files, call models, or interpret record text as authority.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Reference struct {
	Owner    string `json:"owner"`
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Hash     string `json:"hash"`
}
type Input struct {
	ID         string      `json:"id"`
	Owner      string      `json:"owner"`
	Author     string      `json:"author"`
	Subject    string      `json:"subject"`
	Kind       string      `json:"kind"`
	Status     string      `json:"status"`
	Content    string      `json:"content"`
	Context    string      `json:"context"`
	Project    string      `json:"project"`
	Topic      string      `json:"topic"`
	OccurredAt string      `json:"occurred_at"`
	ExpiresAt  string      `json:"expires_at,omitempty"`
	Provenance string      `json:"provenance"`
	Sources    []Reference `json:"sources,omitempty"`
}
type Record struct {
	Input
	Revision   int    `json:"revision"`
	Hash       string `json:"hash"`
	Lifecycle  string `json:"lifecycle"`
	RecordedAt string `json:"recorded_at"`
}
type Filter struct {
	Owners                                                []string
	Kind, Status, Project, Topic, Context, Query, AfterID string
	Limit                                                 int
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ownerDB(owner string) (string, error) {
	switch owner {
	case "athena":
		return "main", nil
	case "dreo":
		return "dreo", nil
	}
	return "", fmt.Errorf("owner must be athena or dreo")
}
func allowed(owners []string, owner string) bool {
	for _, o := range owners {
		if o == owner {
			return true
		}
	}
	return false
}
func validateOwners(owners []string) error {
	if len(owners) < 1 || len(owners) > 2 {
		return fmt.Errorf("explicit one or two owners required")
	}
	seen := map[string]bool{}
	for _, o := range owners {
		if _, e := ownerDB(o); e != nil {
			return e
		}
		if seen[o] {
			return fmt.Errorf("duplicate owner")
		}
		seen[o] = true
	}
	return nil
}
func validText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && len(s) <= max && !strings.ContainsRune(s, 0) && (!required || strings.TrimSpace(s) != "")
}
func member(s, values string) bool {
	for _, v := range strings.Fields(values) {
		if s == v {
			return true
		}
	}
	return false
}
func validateInput(in Input, confirm bool) error {
	if _, e := ownerDB(in.Owner); e != nil {
		return e
	}
	if !identifier.MatchString(in.ID) {
		return fmt.Errorf("invalid record ID")
	}
	if !member(in.Author, "athena dreo other") {
		return fmt.Errorf("invalid author")
	}
	kinds := "episode thought idea dream mistake lesson"
	if in.Owner == "dreo" {
		kinds = "memory mistake preference"
	}
	if !member(in.Kind, kinds) {
		return fmt.Errorf("invalid kind for owner")
	}
	if !member(in.Status, "reported grounded confirmed inferred speculative disputed") {
		return fmt.Errorf("invalid epistemic status")
	}
	if in.Owner == "dreo" && in.Author == "athena" {
		return fmt.Errorf("Athena interpretations belong to Athena, not Dreo")
	}
	if in.Status == "confirmed" && !(confirm && in.Owner == "dreo" && in.Author == "dreo" && in.Kind == "preference") {
		return fmt.Errorf("confirmed preferences require explicit human confirmation and Dreo authorship/ownership")
	}
	if in.Kind == "dream" && !member(in.Status, "inferred speculative") {
		return fmt.Errorf("dreams must remain inferred or speculative")
	}
	if !validText(in.Content, 16384, true) || !validText(in.Provenance, 1024, true) {
		return fmt.Errorf("content or provenance missing, invalid or oversized")
	}
	for _, s := range []string{in.Subject, in.Context, in.Project, in.Topic} {
		if !validText(s, 256, false) {
			return fmt.Errorf("invalid metadata")
		}
	}
	at, e := time.Parse(time.RFC3339Nano, in.OccurredAt)
	if e != nil || at.Year() < 1970 || at.Year() > 2200 {
		return fmt.Errorf("occurred_at must be RFC3339 within 1970..2200")
	}
	if in.ExpiresAt != "" {
		expires, e := time.Parse(time.RFC3339Nano, in.ExpiresAt)
		if e != nil || expires.Year() > 2200 || !expires.After(at) {
			return fmt.Errorf("expires_at must follow occurred_at")
		}
	}
	if len(in.Sources) > 16 {
		return fmt.Errorf("too many sources")
	}
	// Internal source edges describe derived artifacts, never another owner's
	// independent recollection. External provenance remains a supplied label.
	if len(in.Sources) > 0 && (in.Owner != "athena" || !member(in.Kind, "thought idea dream lesson")) {
		return fmt.Errorf("tracked derivations are Athena thoughts, ideas, dreams or lessons only")
	}
	seen := map[string]bool{}
	for _, r := range in.Sources {
		if _, e := ownerDB(r.Owner); e != nil {
			return e
		}
		if !identifier.MatchString(r.ID) || r.Revision < 1 || len(r.Hash) != 64 {
			return fmt.Errorf("invalid source reference")
		}
		if _, e := hex.DecodeString(r.Hash); e != nil {
			return fmt.Errorf("invalid source hash")
		}
		key := r.Owner + "/" + r.ID
		if seen[key] || (r.Owner == in.Owner && r.ID == in.ID) {
			return fmt.Errorf("duplicate or self source")
		}
		seen[key] = true
	}
	return nil
}
func inputHash(in Input) string {
	b, _ := json.Marshal(in)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func expiresUnix(in Input) int64 {
	if in.ExpiresAt == "" {
		return 0
	}
	t, _ := time.Parse(time.RFC3339Nano, in.ExpiresAt)
	return t.UnixNano()
}
