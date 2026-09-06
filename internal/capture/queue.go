// Package capture owns immutable candidate revisions and atomic processing receipts.
package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = 2
const MaxReportBytes = 16384

type Candidate struct {
	Version        int           `json:"version"`
	ID             string        `json:"id"`
	Source         string        `json:"source"`
	Kind           string        `json:"kind"`
	Text           string        `json:"text"`
	Refs           []string      `json:"refs"`
	CapturedAt     string        `json:"captured_at"`
	OccurredAt     string        `json:"occurred_at"`
	TimeBasis      string        `json:"time_basis"`
	Context        event.Context `json:"context"`
	Origin         string        `json:"origin"`
	Internal       bool          `json:"internal"`
	NativeID       string        `json:"native_id,omitempty"`
	Revision       string        `json:"revision,omitempty"`
	IdempotencyKey string        `json:"idempotency_key,omitempty"`
	Evidence       []string      `json:"evidence"`
	Completeness   string        `json:"completeness"`
	Terminal       string        `json:"terminal"`
	Episode        string        `json:"episode"`
}
type Evidence struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Hash       string `json:"hash"`
	Text       string `json:"text"`
	Kind       string `json:"kind"`
	CapturedAt string `json:"captured_at"`
}
type Receipt struct {
	Version       int      `json:"version"`
	CandidateID   string   `json:"candidate_id"`
	Status        string   `json:"status"`
	Disposition   string   `json:"disposition,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Attempts      int      `json:"attempts"`
	UpdatedAt     string   `json:"updated_at"`
	NextAttemptAt string   `json:"next_attempt_at,omitempty"`
	InputHash     string   `json:"input_hash,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	Model         string   `json:"model,omitempty"`
	Policy        string   `json:"policy,omitempty"`
	PlanID        string   `json:"plan_id,omitempty"`
	AppliedEvents []string `json:"applied_events"`
}
type Item struct {
	Candidate Candidate `json:"candidate"`
	Receipt   Receipt   `json:"receipt"`
}
type Queue interface {
	Enqueue(Candidate) (Candidate, error)
	List(string) ([]Item, error)
	SaveReceipt(Receipt) error
}
type Spool struct{ Root string }

func Open() (*Spool, error) {
	if err := store.Ensure(); err != nil {
		return nil, err
	}
	r, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(r, "capture")
	for _, d := range []string{"candidates", "evidence", "receipts", "plans", "cursors"} {
		if err := durable.Mkdir(filepath.Join(root, d)); err != nil {
			return nil, err
		}
	}
	return &Spool{root}, nil
}
func Hash(b []byte) string  { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func JSONHash(v any) string { b, _ := json.Marshal(v); return Hash(b) }
func (c Candidate) Validate() error {
	if c.Version != Version || !SafeID(c.ID) {
		return fmt.Errorf("unsupported/incomplete candidate")
	}
	if err := event.ValidateSource(c.Source); err != nil {
		return err
	}
	if !strings.HasPrefix(c.Source, "agent:") || !event.Narrative(c.Kind) {
		return fmt.Errorf("candidate requires agent narrative")
	}
	if strings.TrimSpace(c.Text) == "" || len(c.Text) > MaxReportBytes || !utf8.ValidString(c.Text) {
		return fmt.Errorf("report must contain 1..%d UTF-8 bytes", MaxReportBytes)
	}
	if c.Internal {
		return fmt.Errorf("internal Athena input excluded")
	}
	if c.Origin != "report" && c.Origin != "hook" && c.Origin != "recovery" {
		return fmt.Errorf("unsupported origin")
	}
	if c.Completeness != "claim" && c.Completeness != "partial" && c.Completeness != "complete" {
		return fmt.Errorf("invalid completeness")
	}
	if c.Terminal != "unknown" && c.Terminal != "completed" && c.Terminal != "interrupted" {
		return fmt.Errorf("invalid terminal state")
	}
	for _, ts := range []string{c.CapturedAt, c.OccurredAt} {
		if _, e := time.Parse(time.RFC3339Nano, ts); e != nil {
			return e
		}
	}
	if c.TimeBasis != "report" && c.TimeBasis != "native" {
		return fmt.Errorf("invalid time basis")
	}
	if len(c.Refs) > 32 || len(c.Evidence) > 32 || len(c.IdempotencyKey) > 512 || len(c.NativeID) > 512 || len(c.Revision) > 128 {
		return fmt.Errorf("candidate metadata too large")
	}
	for _, r := range c.Refs {
		if _, e := event.NormalizeRef(r, event.Repository{}); e != nil {
			return e
		}
	}
	return nil
}
func Episode(c Candidate) string {
	repo := c.Context.Repository.Key()
	if repo == "" {
		repo = c.Context.Worktree
	}
	if repo == "" {
		repo = c.Context.Cwd
	}
	if c.Context.Task != "" {
		return Hash([]byte(repo + "/task/" + c.Context.Task))
	}
	if c.Context.Session != "" && c.Context.Turn != "" {
		return Hash([]byte(repo + "/" + c.Source + "/" + c.Context.Session + "/" + c.Context.Turn))
	}
	// Session or repository alone is not a task identity.
	return c.ID
}
func (s *Spool) Enqueue(c Candidate) (Candidate, error) {
	if c.ID == "" {
		c.ID = event.NewID(time.Now())
	}
	if c.IdempotencyKey != "" {
		c.ID = "c-" + Hash([]byte(c.Source+"/key/"+c.IdempotencyKey))
	} else if c.NativeID != "" && c.Revision != "" && c.Context.Session != "" {
		c.ID = "c-" + Hash([]byte(c.Source+"/"+c.Context.Session+"/"+c.NativeID+"/"+c.Revision))
	}
	c.Episode = Episode(c)
	if err := c.Validate(); err != nil {
		return Candidate{}, err
	}
	// Separate from the worker/ledger locks: enqueue never waits on model work.
	unlock, err := durable.Lock(filepath.Join(s.Root, "intake.lock"), true)
	if err != nil {
		return Candidate{}, err
	}
	defer unlock()
	p := filepath.Join(s.Root, "candidates", c.ID+".json")
	var existing Candidate
	if err := durable.Read(p, &existing); err == nil {
		if err := existing.Validate(); err != nil {
			return Candidate{}, err
		}
		if existing.Text != c.Text || existing.Kind != c.Kind || JSONHash(existing.Refs) != JSONHash(c.Refs) {
			return Candidate{}, fmt.Errorf("idempotency identity reused for different content")
		}
		return existing, nil
	} else if !os.IsNotExist(err) {
		return Candidate{}, err
	}
	for _, id := range c.Evidence {
		if _, err := s.GetEvidence(id); err != nil {
			return Candidate{}, err
		}
	}
	if err := durable.JSON(p, c); err != nil {
		return Candidate{}, err
	}
	return c, nil
}
func (r Receipt) Validate() error {
	if !SafeID(r.CandidateID) || r.Version != Version || (r.PlanID != "" && !SafeID(r.PlanID)) || r.Attempts < 0 {
		return fmt.Errorf("invalid receipt identity/version")
	}
	switch r.Status {
	case "pending", "processing", "processed", "error":
	default:
		return fmt.Errorf("invalid processing state")
	}
	switch r.Disposition {
	case "", "outcome", "skip", "hold":
	default:
		return fmt.Errorf("invalid editorial disposition")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.UpdatedAt); err != nil {
		return fmt.Errorf("invalid receipt timestamp")
	}
	return nil
}
func (s *Spool) SaveReceipt(r Receipt) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return durable.JSON(filepath.Join(s.Root, "receipts", r.CandidateID+".json"), r)
}
func SafeID(id string) bool {
	return id != "" && len(id) < 160 && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) < 0
}
func (s *Spool) List(status string) ([]Item, error) {
	if status != "" && status != "pending" && status != "processing" && status != "processed" && status != "error" {
		return nil, fmt.Errorf("invalid queue status")
	}
	paths, err := filepath.Glob(filepath.Join(s.Root, "candidates", "*.json"))
	if err != nil {
		return nil, err
	}
	out := []Item{}
	for _, p := range paths {
		var c Candidate
		if err := durable.Read(p, &c); err != nil {
			return nil, err
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if !SafeID(c.ID) || filepath.Base(p) != c.ID+".json" {
			return nil, fmt.Errorf("candidate filename mismatch")
		}
		r := Receipt{Version: Version, CandidateID: c.ID, Status: "pending", UpdatedAt: c.CapturedAt, AppliedEvents: []string{}}
		if err := durable.Read(filepath.Join(s.Root, "receipts", c.ID+".json"), &r); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if r.Version != Version || r.CandidateID != c.ID {
			return nil, fmt.Errorf("unsupported receipt")
		}
		if err := r.Validate(); err != nil {
			return nil, err
		}
		if status == "" || status == r.Status {
			out = append(out, Item{c, r})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, out[i].Candidate.CapturedAt)
		b, _ := time.Parse(time.RFC3339Nano, out[j].Candidate.CapturedAt)
		if a.Equal(b) {
			return out[i].Candidate.ID < out[j].Candidate.ID
		}
		return a.Before(b)
	})
	return out, nil
}
func (s *Spool) PutEvidence(text, kind, at string) (Evidence, error) {
	text = Redact(text, MaxReportBytes)
	if text == "" || len(text) > MaxReportBytes {
		return Evidence{}, fmt.Errorf("evidence excerpt exceeds bounds")
	}
	h := Hash([]byte(text))
	e := Evidence{Version: Version, ID: "e-" + h, Hash: h, Text: text, Kind: kind, CapturedAt: at}
	return e, durable.JSON(filepath.Join(s.Root, "evidence", e.ID+".json"), e)
}
func (s *Spool) GetEvidence(id string) (Evidence, error) {
	var e Evidence
	if !SafeID(id) {
		return e, fmt.Errorf("invalid evidence ID")
	}
	if err := durable.Read(filepath.Join(s.Root, "evidence", id+".json"), &e); err != nil {
		return e, err
	}
	if e.Version != Version || e.ID != id || Hash([]byte(e.Text)) != e.Hash {
		return e, fmt.Errorf("evidence hash/version mismatch")
	}
	return e, nil
}
