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
	// A private settings home disables global retries, prompts, and resources while
	// using the installed pi auth command for locked credential refresh. Never
	// symlink auth.json: pi locks that lexical path, which would race the real harness.
	tmp, err := os.MkdirTemp("", "daylog-athena-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(tmp)
	if err := durable.Private(tmp, true); err != nil {
		return out, err
	}
	agent := filepath.Join(tmp, "agent")
	if err := durable.Mkdir(agent); err != nil {
		return out, err
	}
	authDir := c.AgentDir
	if authDir == "" {
		return out, fmt.Errorf("Athena unavailable: point runner.agent_dir at your existing pi configuration; reports remain queued")
	}
	for _, name := range []string{"models.json", "models-store.json"} {
		data, err := os.ReadFile(filepath.Join(authDir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return out, err
		}
		if len(data) > 4*1024*1024 {
			return out, fmt.Errorf("pi catalog exceeds 4 MiB")
		}
		if err := durable.Write(filepath.Join(agent, name), data); err != nil {
			return out, err
		}
	}
	if err := durable.JSON(filepath.Join(agent, "settings.json"), map[string]any{"retry": map[string]any{"enabled": false, "maxRetries": 0, "provider": map[string]any{"maxRetries": 0}}, "compaction": map[string]any{"enabled": false}, "enableInstallTelemetry": false}); err != nil {
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
	authArgs := []string{"auth", "print-api-key", "--provider", c.Provider, "--no-extensions", "--no-skills", "--no-context-files", "--no-prompt-templates", "--no-approve", "--offline"}
	if c.CredentialType == "oauth" {
		authArgs[1] = "print-bearer-token"
		authArgs = append(authArgs, "--min-expiry", fmt.Sprintf("%ds", c.TimeoutSeconds+60))
	}
	auth := exec.CommandContext(ctx, c.Binary, append(append([]string{}, c.Arguments...), authArgs...)...)
	auth.Dir = tmp
	auth.WaitDelay = 2 * time.Second
	auth.Env = append(append([]string{}, env...), "PATH="+c.Path, "PI_CODING_AGENT_DIR="+authDir, "DAYLOG_INTERNAL=1", "PI_OFFLINE=1", "PI_TELEMETRY=0")
	token := &capBuffer{max: 16384}
	auth.Stdout = token
	auth.Stderr = &capBuffer{max: 8192}
	if err := auth.Run(); err != nil {
		return out, fmt.Errorf("Athena could not access the configured model %s/%s: could not use the installed pi harness or its existing authentication (%w); run daylog doctor --check-model; reports remain queued", c.Provider, c.Model, err)
	}
	key := strings.TrimSpace(token.String())
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return out, fmt.Errorf("pi returned an invalid credential")
	}
	credential := map[string]any{"type": "api_key", "key": key}
	if c.CredentialType == "oauth" {
		credential = map[string]any{"type": "oauth", "access": key, "refresh": "", "expires": time.Now().Add(time.Duration(c.TimeoutSeconds+30) * time.Second).UnixMilli()}
	}
	if err := durable.JSON(filepath.Join(agent, "auth.json"), map[string]any{c.Provider: credential}); err != nil {
		return out, err
	}
	cmd.Env = append(env, "PATH="+c.Path, "PI_CODING_AGENT_DIR="+agent, "DAYLOG_INTERNAL=1", "DAYLOG_SOURCE=agent:daylog-athena", "PI_OFFLINE=1", "PI_TELEMETRY=0")
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
	final := ""
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
		if e.Type == "message_end" && e.Message.Role == "assistant" {
			if e.Message.StopReason != "stop" {
				return out, fmt.Errorf("pi assistant did not finish normally: %s", e.Message.StopReason)
			}
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
	if !ended || final == "" {
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
