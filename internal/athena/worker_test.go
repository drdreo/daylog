package athena

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls int
	fn    func(Input) (Output, error)
}

func (f *fakeRunner) Run(_ context.Context, in Input) (Output, error) { f.calls++; return f.fn(in) }
func fixture(t *testing.T, mode string) (*Worker, *fakeRunner, capture.Candidate) {
	t.Helper()
	t.Setenv("DAYLOG_DIR", filepath.Join(t.TempDir(), "data"))
	q, e := capture.Open()
	if e != nil {
		t.Fatal(e)
	}
	cwd := t.TempDir()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	c, e := q.Enqueue(capture.Candidate{Version: 2, Source: "agent:claude", Kind: "work", Text: "Fixed a race", Context: event.Context{Cwd: cwd, Task: "refresh", Repository: event.Repository{Host: "github.com", Path: "o/r"}}, CapturedAt: now.Add(-10 * time.Minute).Format(time.RFC3339), OccurredAt: now.Add(-10 * time.Minute).Format(time.RFC3339), TimeBasis: "report", Origin: "report", Completeness: "claim", Terminal: "completed"})
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Defaults()
	cfg.Mode = mode
	cfg.CloudProjects = []string{cwd}
	f := &fakeRunner{fn: func(in Input) (Output, error) {
		return Output{Version: 2, Actions: []Action{{Kind: "publish", Candidates: []string{in.Candidates[0].ID}, Type: "work", Text: "Fixed refresh locking", Reason: "material change", Refs: []string{}}}}, nil
	}}
	return &Worker{Queue: q, Config: cfg, Runner: f, Now: func() time.Time { return now }}, f, c
}
func TestReplayAfterAppendBeforeAck(t *testing.T) {
	w, f, _ := fixture(t, "live")
	w.AfterAppend = func() error { return errors.New("simulated process death") }
	if _, e := w.Once(context.Background(), false); e == nil {
		t.Fatal("fault ignored")
	}
	w.AfterAppend = nil
	if _, e := w.Once(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	all, e := store.ReadAll()
	if e != nil || len(all) != 1 || f.calls != 1 {
		t.Fatal(all, e, f.calls)
	}
	if all[0].Source != "agent:claude" || all[0].Provenance.Model != "openai-codex/gpt-5.6-luna" {
		t.Fatal(all[0])
	}
	items, _ := w.Queue.List("processed")
	if len(items) != 1 || len(items[0].Receipt.AppliedEvents) != 1 {
		t.Fatal(items)
	}
}
func TestHoldNotRetriedByUnrelatedArrival(t *testing.T) {
	w, f, c := fixture(t, "live")
	f.fn = func(in Input) (Output, error) {
		acts := []Action{}
		for _, c := range in.Candidates {
			acts = append(acts, Action{Kind: "hold", Candidates: []string{c.ID}, Reason: "unverified"})
		}
		return Output{Version: 2, Actions: acts}, nil
	}
	w.Once(context.Background(), false)
	w.Once(context.Background(), false)
	if f.calls != 1 {
		t.Fatal(f.calls)
	}
	c.ID = ""
	c.Context.Task = "other"
	w.Queue.Enqueue(c)
	w.Once(context.Background(), false)
	if f.calls != 2 {
		t.Fatal(f.calls)
	}
	c.ID = ""
	c.Context.Task = "refresh"
	w.Queue.Enqueue(c)
	var inputs int
	old := f.fn
	f.fn = func(in Input) (Output, error) { inputs = len(in.Candidates); return old(in) }
	w.Once(context.Background(), false)
	if inputs != 2 {
		t.Fatalf("new episode evidence should reopen held input: %d", inputs)
	}
}
func TestModelFailureAndCloudConsent(t *testing.T) {
	w, f, _ := fixture(t, "live")
	f.fn = func(Input) (Output, error) { return Output{}, errors.New("unavailable") }
	if _, e := w.Once(context.Background(), false); e == nil {
		t.Fatal("missing error")
	}
	w.Once(context.Background(), false)
	if f.calls != 1 {
		t.Fatal("backoff ignored")
	}
	items, _ := w.Queue.List("error")
	if len(items) != 1 {
		t.Fatal(items)
	}
	all, _ := store.ReadAll()
	if len(all) != 0 {
		t.Fatal("fallback publication")
	}
	w, f, _ = fixture(t, "live")
	w.Config.CloudProjects = nil
	if _, e := w.Once(context.Background(), false); e == nil || f.calls != 0 {
		t.Fatal("cloud consent bypass", e)
	}
}
func TestStrictHostileOutput(t *testing.T) {
	w, _, c := fixture(t, "live")
	in, e := w.input([]capture.Item{{Candidate: c}})
	if e != nil {
		t.Fatal(e)
	}
	good := Action{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "fixed", Reason: "supported"}
	cases := []Action{good, good, good, good, good}
	cases[0].Candidates = []string{"invented"}
	cases[1].Evidence = []string{"invented"}
	cases[2].Refs = []string{"linear:ABC-1"}
	cases[3].Type = "todo"
	cases[4].Targets = []event.Target{{ID: "human", Revision: 1}}
	for _, a := range cases {
		if Validate(in, Output{Version: 2, Actions: []Action{a}}) == nil {
			t.Fatal(a)
		}
	}
	if Validate(in, Output{Version: 2, Actions: []Action{good, {Kind: "skip", Candidates: []string{c.ID}, Reason: "conflict"}}}) == nil {
		t.Fatal("conflict")
	}
}
func TestPiEventParserAndIsolationArgs(t *testing.T) {
	text := `{"version":2,"actions":[]}`
	message := map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "stopReason": "stop", "content": []any{map[string]string{"type": "text", "text": text}}}}
	b, _ := json.Marshal(message)
	b = append(b, []byte("\n{\"type\":\"agent_end\"}\n")...)
	if _, e := ParseEvents(b); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`{"version":2,"actions":[]} trailing`, `{"version":2,"actions":[],"evil":true}`} {
		message["message"].(map[string]any)["content"] = []any{map[string]string{"type": "text", "text": bad}}
		b, _ := json.Marshal(message)
		b = append(b, []byte("\n{\"type\":\"agent_end\"}\n")...)
		if _, e := ParseEvents(b); e == nil {
			t.Fatal(bad)
		}
	}
	args := strings.Join(Args(config.Defaults().Runner), " ")
	for _, flag := range []string{"--no-tools", "--no-session", "--no-context-files", "--no-extensions", "--no-skills", "--no-prompt-templates", "--append-system-prompt", "--no-approve"} {
		if !strings.Contains(args, flag) {
			t.Fatal(flag)
		}
	}
}
func TestGoldenEpisodes(t *testing.T) {
	b, e := os.ReadFile("../../testdata/gatekeeper/episodes.json")
	if e != nil {
		t.Fatal(e)
	}
	var episodes []struct {
		Name     string `json:"name"`
		Report   string `json:"report"`
		Decision string `json:"decision"`
		Text     string `json:"text"`
	}
	if e := json.Unmarshal(b, &episodes); e != nil {
		t.Fatal(e)
	}
	if len(episodes) < 7 {
		t.Fatal("missing labeled episodes")
	}
	for _, ep := range episodes {
		if ep.Report == "" || ep.Decision == "" {
			t.Fatal(ep)
		}
	}
}
