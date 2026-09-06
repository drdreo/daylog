// Package adapters normalizes the explicitly supported harness contracts.
package adapters

import (
	"encoding/json"
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	original "github.com/drdreo/daylog/internal/context"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var Versions = map[string]string{"pi": "0.85.1", "claude": "2.1.224", "codex": "0.153.2"}

type Hook struct {
	AdapterVersion  int    `json:"adapter_version"`
	HarnessVersion  string `json:"harness_version"`
	Event           string `json:"hook_event_name"`
	Session         string `json:"session_id"`
	Turn            string `json:"turn_id"`
	Prompt          string `json:"prompt_id"`
	Cwd             string `json:"cwd"`
	Transcript      string `json:"transcript_path"`
	AgentTranscript string `json:"agent_transcript_path"`
	AgentID         string `json:"agent_id"`
	Text            string `json:"last_assistant_message"`
	NativeID        string `json:"native_id"`
	Timestamp       string `json:"timestamp"`
	Terminal        string `json:"terminal"`
	Parent          string `json:"parent_session"`
	Task            string `json:"task_id"`
	Internal        bool   `json:"internal"`
}

func Approved(cfg config.Config, harness, cwd, path string) bool {
	root, _ := store.DataDir()
	if original.Within(cwd, root) || (path != "" && original.Within(path, root)) {
		return false
	}
	for _, s := range cfg.CaptureScopes {
		if s.Harness != harness {
			continue
		}
		if path != "" && !original.Within(path, s.Directory) {
			continue
		}
		for _, p := range s.Projects {
			if original.Within(cwd, p) {
				return true
			}
		}
	}
	return false
}
func ParseHook(harness, version string, b []byte) (Hook, error) {
	var h Hook
	if Versions[harness] != version {
		return h, fmt.Errorf("unsupported %s version %q; supported %s", harness, version, Versions[harness])
	}
	if len(b) > 65536 {
		return h, fmt.Errorf("hook exceeds 64 KiB")
	}
	if !json.Valid(b) {
		return h, fmt.Errorf("invalid hook JSON")
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return h, err
	}
	if h.Session == "" || !filepath.IsAbs(h.Cwd) {
		return h, fmt.Errorf("hook requires native session and absolute cwd")
	}
	if harness == "pi" {
		if h.AdapterVersion != 1 || h.HarnessVersion != version || (h.Event != "agent_settled" && h.Event != "SessionStart") || (h.Event == "agent_settled" && h.NativeID == "") {
			return h, fmt.Errorf("unsupported pi settled envelope")
		}
	} else {
		switch h.Event {
		case "Stop", "SubagentStop", "SessionStart":
		case "StopFailure":
			if harness != "claude" {
				return h, fmt.Errorf("unsupported terminal hook")
			}
		case "Interrupt":
			if harness != "codex" {
				return h, fmt.Errorf("unsupported terminal hook")
			}
		default:
			return h, fmt.Errorf("unsupported hook event %q", h.Event)
		}
	}
	return h, nil
}
func CaptureHook(q *capture.Spool, cfg config.Config, harness, version string, b []byte, now time.Time) (capture.Candidate, error) {
	h, err := ParseHook(harness, version, b)
	if err != nil {
		return capture.Candidate{}, err
	}
	if h.Internal || os.Getenv("DAYLOG_INTERNAL") == "1" {
		return capture.Candidate{}, fmt.Errorf("internal Athena process excluded")
	}
	if !Approved(cfg, harness, h.Cwd, "") {
		return capture.Candidate{}, fmt.Errorf("project not approved for %s capture", harness)
	}
	ctx := original.At(h.Cwd)
	ctx.Session = h.Session
	ctx.Turn = h.Turn
	if harness == "claude" {
		ctx.Turn = h.Prompt
	}
	ctx.Task = h.Task
	ctx.ParentSession = h.Parent
	if h.AgentID != "" {
		ctx.ParentSession = h.Session
		ctx.Session = h.AgentID
	}
	terminal := h.Terminal
	if terminal == "" {
		terminal = "completed"
	}
	if h.Event == "Interrupt" || h.Event == "StopFailure" {
		terminal = "interrupted"
	}
	text := h.Text
	if strings.TrimSpace(text) == "" {
		text = "Terminal boundary observed without saved assistant text; result unknown."
		terminal = "interrupted"
	}
	at := h.Timestamp
	basis := "native"
	if _, e := time.Parse(time.RFC3339Nano, at); e != nil {
		at = now.Format(time.RFC3339Nano)
		basis = "report"
	}
	return enqueue(q, harness, ctx, text, h.NativeID, at, basis, terminal, "hook", now)
}

// Only assistant text, never reasoning/tool payloads, reaches this boundary.
func Redact(text string) string { return capture.Redact(text, 8192) }
func enqueue(q *capture.Spool, harness string, ctx event.Context, text, native, at, basis, terminal, origin string, now time.Time) (capture.Candidate, error) {
	text = Redact(text)
	if text == "" {
		return capture.Candidate{}, fmt.Errorf("no usable assistant text")
	}
	rev := capture.Hash([]byte(text + "/" + terminal))
	key := ""
	if harness == "pi" {
		if native == "" {
			return capture.Candidate{}, fmt.Errorf("missing pi node identity")
		}
		key = "pi/node/" + capture.Hash([]byte(ctx.Cwd+"/"+native+"/"+at+"/"+rev))
	} else if harness == "codex" && ctx.Turn != "" {
		native = "turn/" + ctx.Turn
	} else {
		native = "assistant/" + capture.Hash([]byte(text))
	}
	e, err := q.PutEvidence(text, "assistant-claim", at)
	if err != nil {
		return capture.Candidate{}, err
	}
	c := capture.Candidate{Version: 2, Source: "agent:" + harness, Kind: "work", Text: "Native assistant report (" + terminal + "); see the separately retained claim excerpt.", Context: ctx, CapturedAt: now.Format(time.RFC3339Nano), OccurredAt: at, TimeBasis: basis, Origin: origin, NativeID: native, Revision: rev, IdempotencyKey: key, Evidence: []string{e.ID}, Refs: []string{}, Completeness: "claim", Terminal: terminal}
	return q.Enqueue(c)
}
