package athena

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed policy.txt
var Policy string

type Runner interface {
	Run(context.Context, Input) (Output, error)
}
type PiRunner struct{ Config config.Runner }
type capBuffer struct {
	bytes.Buffer
	max int
}

func (b *capBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("pi output exceeds cap")
	}
	return b.Buffer.Write(p)
}

// Args supplies explicit empty append policy: --no-context-files alone does NOT
// disable global APPEND_SYSTEM.md in pi 0.85.1.
func Args(c config.Runner) []string {
	return []string{"--print", "--mode", "json", "--no-session", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve", "--offline", "--provider", c.Provider, "--model", c.Model, "--thinking", "low", "--system-prompt", Policy, "--append-system-prompt", ""}
}
func (r PiRunner) Run(parent context.Context, in Input) (Output, error) {
	var out Output
	c := r.Config
	if c.Binary == "" || !filepath.IsAbs(c.Binary) {
		return out, fmt.Errorf("Athena unavailable: select your installed pi with daylog setup --pi /absolute/path/to/pi; reports remain queued")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	if len(b) > c.MaxInputBytes {
		return out, fmt.Errorf("Athena input exceeds cap")
	}
	// Run outside any project. Pi owns its normal model catalog, credentials,
	// and refresh locking; CLI flags disable tools and prompt/resource discovery.
	tmp, err := os.MkdirTemp("", "daylog-athena-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(tmp)
	if err := durable.Private(tmp, true); err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Binary, append(append([]string{}, c.Arguments...), Args(c)...)...)
	cmd.Dir = tmp
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(b)
	env := []string{}
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "DAYLOG_") || strings.HasPrefix(k, "PI_") || k == "PATH" {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, "PATH="+c.Path, "DAYLOG_INTERNAL=1", "DAYLOG_SOURCE=agent:daylog-athena", "PI_OFFLINE=1", "PI_TELEMETRY=0")
	if c.AgentDir != "" {
		cmd.Env = append(cmd.Env, "PI_CODING_AGENT_DIR="+c.AgentDir)
	}
	stdout := &capBuffer{max: c.MaxOutputBytes}
	stderr := &capBuffer{max: 8192}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("pi timeout/cancellation: %w", ctx.Err())
		}
		return out, fmt.Errorf("Athena's configured model %s/%s is unavailable or failed: %w; run daylog doctor --check-model using your existing pi setup; reports remain queued (provider stderr withheld for privacy)", c.Provider, c.Model, err)
	}
	return ParseEvents(stdout.Bytes())
}

// ParseEvents reads authoritative message_end events, never streaming deltas.
func ParseEvents(b []byte) (Output, error) {
	var out Output
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 4096), 4*1024*1024)
	final, stopReason := "", ""
	ended := false
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return out, fmt.Errorf("invalid pi JSON event: %w", err)
		}
		if strings.HasPrefix(e.Type, "tool_execution") {
			return out, fmt.Errorf("Athena's isolated model attempted a tool")
		}
		// Pi can emit a failed assistant turn, then retry successfully within the
		// same process. Never accept an earlier result if a later turn is unfinished.
		if e.Type == "agent_start" || e.Type == "turn_start" || e.Type == "message_start" || e.Type == "auto_retry_start" {
			final, stopReason, ended = "", "", false
		}
		if e.Type == "message_end" && e.Message.Role == "assistant" {
			stopReason, ended = e.Message.StopReason, false
			var text strings.Builder
			for _, part := range e.Message.Content {
				if part.Type == "toolCall" {
					return out, fmt.Errorf("Athena model tool call rejected")
				}
				if part.Type == "text" {
					text.WriteString(part.Text)
				}
			}
			final = text.String()
		}
		if e.Type == "agent_end" {
			ended = true
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	if !ended || stopReason == "" {
		return out, fmt.Errorf("incomplete pi event stream")
	}
	if stopReason != "stop" {
		return out, fmt.Errorf("pi assistant did not finish normally: %s", stopReason)
	}
	if final == "" {
		return out, fmt.Errorf("incomplete pi event stream")
	}
	if len(final) > 65536 {
		return out, fmt.Errorf("Athena decision JSON too large")
	}
	if err := durable.Decode([]byte(final), &out); err != nil {
		return out, fmt.Errorf("invalid Athena decision JSON: %w", err)
	}
	return out, nil
}

var _ io.Writer = (*capBuffer)(nil)
