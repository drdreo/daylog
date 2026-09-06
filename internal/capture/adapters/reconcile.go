package adapters

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	original "github.com/drdreo/daylog/internal/context"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ParserVersion = 1

type SavedContext struct {
	Version int           `json:"version"`
	Context event.Context `json:"context"`
}

type Cursor struct {
	Version        int               `json:"version"`
	Harness        string            `json:"harness"`
	Path           string            `json:"path"`
	Offset         int64             `json:"offset"`
	HeaderHash     string            `json:"header_hash"`
	Context        event.Context     `json:"context"`
	Turns          map[string]string `json:"turns"`
	LastAssistant  string            `json:"last_assistant"`
	LastObservedAt string            `json:"last_observed_at"`
	Error          string            `json:"error,omitempty"`
}
type ScanResult struct {
	Files      int      `json:"files"`
	Candidates int      `json:"candidates"`
	Errors     []string `json:"errors"`
	Gaps       string   `json:"gaps"`
}

func Reconcile(q *capture.Spool, cfg config.Config, now time.Time) (ScanResult, error) {
	res := ScanResult{Errors: []string{}, Gaps: "Only saved supported transcripts in approved scopes; unsaved sessions and hard crashes without evidence cannot be recovered."}
	unlock, err := durable.Lock(filepath.Join(q.Root, "reconcile.lock"), false)
	if err != nil {
		return res, err
	}
	defer unlock()
	paths := map[string]string{}
	visited := 0
	for _, scope := range cfg.CaptureScopes {
		err := filepath.WalkDir(scope.Directory, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			visited++
			if visited > 10000 {
				return fmt.Errorf("approved scan exceeds 10000 paths; narrow scope")
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".jsonl") {
				paths[p] = scope.Harness
			}
			return nil
		})
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
		}
	}
	ordered := []string{}
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	// Rotate the bounded path scan so busy/large directories do not starve later files.
	var rotation struct {
		Version int    `json:"version"`
		After   string `json:"after"`
	}
	rotation.Version = ParserVersion
	rp := filepath.Join(q.Root, "cursors", "scan.json")
	if err := durable.Read(rp, &rotation); err != nil && !os.IsNotExist(err) {
		return res, err
	}
	start := sort.SearchStrings(ordered, rotation.After)
	if start < len(ordered) && ordered[start] == rotation.After {
		start++
	}
	ordered = append(ordered[start:], ordered[:start]...)
	for _, p := range ordered {
		if res.Files >= 64 {
			break
		}
		h := paths[p]
		n, e := ScanFile(q, cfg, h, p, now)
		res.Files++
		res.Candidates += n
		if e != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, e))
		}
		rotation.After = p
	}
	if err := durable.JSON(rp, rotation); err != nil {
		return res, err
	}
	if len(res.Errors) > 0 {
		return res, fmt.Errorf("reconciliation found %d source gap(s)", len(res.Errors))
	}
	return res, nil
}
func ScanFile(q *capture.Spool, cfg config.Config, harness, path string, now time.Time) (count int, retErr error) {
	curPath := filepath.Join(q.Root, "cursors", capture.Hash([]byte(harness+"/"+path))+".json")
	cur := Cursor{Version: ParserVersion, Harness: harness, Path: path, Turns: map[string]string{}}
	if err := durable.Read(curPath, &cur); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if cur.Version != ParserVersion || cur.Harness != harness {
		return 0, fmt.Errorf("unsupported cursor/parser version")
	}
	defer func() {
		cur.LastObservedAt = now.Format(time.RFC3339Nano)
		cur.Error = ""
		if retErr != nil {
			cur.Error = retErr.Error()
		}
		if e := durable.JSON(curPath, cur); e != nil {
			retErr = e
		}
	}()
	// Validate the approved native path before opening it. Cwd approval follows header parsing.
	pathOK := false
	for _, s := range cfg.CaptureScopes {
		if s.Harness == harness && original.Within(path, s.Directory) {
			pathOK = true
		}
	}
	if !pathOK {
		return 0, fmt.Errorf("unapproved transcript path")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, fmt.Errorf("not a regular transcript")
	}
	r := bufio.NewReaderSize(f, 1024*1024)
	first, err := r.ReadSlice('\n')
	if err != nil {
		return 0, fmt.Errorf("incomplete or oversized transcript header")
	}
	hash := capture.Hash(first)
	if cur.HeaderHash != "" && (cur.HeaderHash != hash || info.Size() < cur.Offset) {
		return 0, fmt.Errorf("transcript replaced/truncated; cursor retained for explicit inspection")
	}
	if cur.Offset == 0 {
		if err := header(&cur, first); err != nil {
			return 0, err
		}
		cur.HeaderHash = hash
		if harness == "pi" || harness == "codex" {
			cur.Offset = int64(len(first))
		}
	}
	if !Approved(cfg, harness, cur.Context.Cwd, path) {
		return 0, fmt.Errorf("transcript project outside approved scope")
	}
	var saved SavedContext
	if err := durable.Read(filepath.Join(q.Root, "cursors", "context-"+capture.Hash([]byte(harness+"/"+cur.Context.Session))+".json"), &saved); err == nil {
		if saved.Version != ParserVersion {
			return 0, fmt.Errorf("unsupported original context version")
		}
		if saved.Context.Cwd == cur.Context.Cwd {
			cur.Context.Repository = saved.Context.Repository
			cur.Context.Worktree = saved.Context.Worktree
			cur.Context.Branch = saved.Context.Branch
			cur.Context.Head = saved.Context.Head
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if _, err := f.Seek(cur.Offset, io.SeekStart); err != nil {
		return 0, err
	}
	r.Reset(f)
	processed := 0
	for processed < 2*1024*1024 && count < 128 {
		line, err := r.ReadSlice('\n')
		if err == io.EOF {
			if len(line) > 0 {
				return count, fmt.Errorf("incomplete trailing native record; waiting for transcript flush")
			}
			break
		}
		if err != nil {
			return count, fmt.Errorf("native record exceeds 1 MiB or read failed: %w", err)
		}
		c, err := native(&cur, line)
		if err != nil {
			return count, err
		}
		if c != nil {
			if !Approved(cfg, harness, c.Context.Cwd, path) {
				return count, fmt.Errorf("native cwd left approved scope")
			}
			if _, err := enqueue(q, harness, c.Context, c.Text, c.NativeID, c.OccurredAt, "native", c.Terminal, "recovery", now); err != nil {
				return count, err
			}
			count++
		}
		cur.Offset += int64(len(line))
		processed += len(line)
	}
	return count, nil
}
func header(c *Cursor, b []byte) error {
	var e struct {
		Type    string          `json:"type"`
		Version json.RawMessage `json:"version"`
		ID      string          `json:"id"`
		Cwd     string          `json:"cwd"`
		Session string          `json:"sessionId"`
		Parent  string          `json:"parentSession"`
		Payload struct {
			ID      string `json:"id"`
			Cwd     string `json:"cwd"`
			Version string `json:"cli_version"`
			Parent  string `json:"parent_thread_id"`
			Git     struct {
				Branch string `json:"branch"`
				Head   string `json:"commit_hash"`
				Remote string `json:"repository_url"`
			} `json:"git"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		return err
	}
	switch c.Harness {
	case "pi":
		if e.Type != "session" || string(e.Version) != "3" {
			return fmt.Errorf("unsupported pi session version")
		}
		c.Context = event.Context{Cwd: e.Cwd, Session: e.ID, ParentSession: e.Parent}
	case "codex":
		if e.Type != "session_meta" || e.Payload.Version != Versions["codex"] {
			return fmt.Errorf("unsupported Codex rollout version")
		}
		p := e.Payload
		c.Context = event.Context{Cwd: p.Cwd, Session: p.ID, ParentSession: p.Parent, Branch: p.Git.Branch, Head: p.Git.Head, Repository: original.Repository(p.Git.Remote)}
	case "claude":
		var version string
		json.Unmarshal(e.Version, &version)
		if version != Versions["claude"] || e.Session == "" {
			return fmt.Errorf("unsupported Claude transcript header/version")
		}
		c.Context = event.Context{Cwd: e.Cwd, Session: e.Session}
	default:
		return fmt.Errorf("unsupported native parser")
	}
	c.Context.Cwd = filepath.Clean(c.Context.Cwd)
	if c.Context.Session == "" || !filepath.IsAbs(c.Context.Cwd) {
		return fmt.Errorf("native header lacks identity/context")
	}
	return nil
}
func textContent(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := []string{}
	for _, p := range blocks {
		if p.Type == "text" || p.Type == "output_text" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}
func native(c *Cursor, b []byte) (*capture.Candidate, error) {
	var e struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		UUID       string `json:"uuid"`
		Parent     string `json:"parentId"`
		ParentUUID string `json:"parentUuid"`
		Timestamp  string `json:"timestamp"`
		Cwd        string `json:"cwd"`
		Version    string `json:"version"`
		Session    string `json:"sessionId"`
		Prompt     string `json:"promptId"`
		GitBranch  string `json:"gitBranch"`
		Message    struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			StopReason string          `json:"stopReason"`
			ClaudeStop string          `json:"stop_reason"`
		} `json:"message"`
		Payload struct {
			Type    string          `json:"type"`
			Turn    string          `json:"turn_id"`
			Cwd     string          `json:"cwd"`
			Last    string          `json:"last_agent_message"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("malformed native record: %w", err)
	}
	ctx := c.Context
	text := ""
	id := e.ID
	terminal := "completed"
	switch c.Harness {
	case "pi":
		if id != "" {
			turn := c.Turns[e.Parent]
			if e.Type == "message" && e.Message.Role == "user" {
				turn = id
			}
			if len(c.Turns) > 20000 {
				return nil, fmt.Errorf("pi ancestry cap reached")
			}
			c.Turns[id] = turn
			ctx.Turn = turn
		}
		if e.Type != "message" || e.Message.Role != "assistant" {
			return nil, nil
		}
		if e.Message.StopReason == "toolUse" {
			return nil, nil
		}
		text = textContent(e.Message.Content)
		if e.Message.StopReason != "stop" {
			terminal = "interrupted"
		}
	case "claude":
		if e.Type != "user" && e.Type != "assistant" {
			return nil, nil
		}
		if e.Version != Versions["claude"] {
			return nil, fmt.Errorf("unsupported Claude record version")
		}
		if e.Cwd != "" {
			ctx.Cwd = filepath.Clean(e.Cwd)
		}
		if e.Session != "" {
			ctx.Session = e.Session
		}
		ctx.Branch = e.GitBranch
		ctx.Turn = e.Prompt
		if e.Type == "user" {
			return nil, nil
		}
		id = e.UUID
		text = textContent(e.Message.Content)
		if e.Message.ClaudeStop == "tool_use" {
			return nil, nil
		}
		if e.Message.ClaudeStop != "end_turn" {
			terminal = "unknown"
		}
	case "codex":
		if e.Type == "turn_context" {
			c.Context.Turn = e.Payload.Turn
			if e.Payload.Cwd != "" {
				c.Context.Cwd = filepath.Clean(e.Payload.Cwd)
			}
			return nil, nil
		}
		if e.Type != "event_msg" {
			return nil, nil
		}
		switch e.Payload.Type {
		case "agent_message":
			c.LastAssistant = Redact(e.Payload.Message)
			return nil, nil
		case "task_complete":
			text = e.Payload.Last
			if len(e.Payload.Error) > 0 && string(e.Payload.Error) != "null" {
				terminal = "interrupted"
			}
		case "turn_aborted":
			text = c.LastAssistant
			terminal = "interrupted"
		default:
			return nil, nil
		}
		if e.Payload.Turn != "" {
			ctx.Turn = e.Payload.Turn
		}
		id = ctx.Turn
		c.LastAssistant = ""
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	if _, err := time.Parse(time.RFC3339Nano, e.Timestamp); err != nil {
		return nil, fmt.Errorf("native outcome missing occurrence time")
	}
	return &capture.Candidate{Context: ctx, Text: text, NativeID: id, OccurredAt: e.Timestamp, Terminal: terminal}, nil
}
