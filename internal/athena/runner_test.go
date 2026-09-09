package athena

import (
	"context"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPiSubprocessIsolationCapsAndTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable; Windows runtime validation still required")
	}
	for _, mode := range []string{"success", "launcher", "oversize", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			authDir := filepath.Join(dir, "installed")
			os.MkdirAll(authDir, 0700)
			for _, name := range []string{"AGENTS.md", "APPEND_SYSTEM.md", "SYSTEM.md"} {
				os.WriteFile(filepath.Join(authDir, name), []byte("MUST NOT ENTER POLICY"), 0600)
			}
			os.WriteFile(filepath.Join(authDir, "settings.json"), []byte(`{"retry":{"maxRetries":99},"extensions":["hostile.ts"]}`), 0600)
			t.Setenv("TEST_EXISTING_PI_DIR", authDir)
			script := `#!/bin/sh
[ "$1" != auth ] || exit 10
[ "$DAYLOG_INTERNAL" = 1 ] || exit 11
[ "$PI_CODING_AGENT_DIR" = "$TEST_EXISTING_PI_DIR" ] || exit 12
[ "$DAYLOG_SOURCE" = agent:daylog-athena ] || exit 13
case "$*" in *--no-session*--no-tools*--no-extensions*--no-skills*--no-prompt-templates*--no-themes*--no-context-files*--no-approve*) ;; *) exit 14;; esac
# Verify the empty append override is an actual argv element, not omitted.
while [ "$#" -gt 0 ]; do
  if [ "$1" = --append-system-prompt ]; then
    shift
    [ "$#" -gt 0 ] && [ -z "$1" ] || exit 15
    break
  fi
  shift
done
[ "$#" -gt 0 ] || exit 16
`
			switch mode {
			case "success", "launcher":
				script += "printf '%s\\n' '{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"stopReason\":\"stop\",\"content\":[{\"type\":\"text\",\"text\":\"{\\\"version\\\":2,\\\"actions\\\":[]}\"}]}}' '{\"type\":\"agent_end\"}'\n"
			case "oversize":
				script += "printf '%100000s' a\n"
			case "timeout":
				script += "sleep 10\n"
			}
			bin := filepath.Join(dir, "fake-pi")
			os.WriteFile(bin, []byte(script), 0700)
			cfg := config.Defaults().Runner
			cfg.Binary = bin
			if mode == "launcher" {
				cfg.Binary = "/bin/sh"
				cfg.Arguments = []string{bin}
			}
			cfg.AgentDir = authDir
			cfg.Path = os.Getenv("PATH")
			cfg.TimeoutSeconds = 10
			if mode == "timeout" {
				cfg.TimeoutSeconds = 1
			}
			cfg.MaxOutputBytes = 2048
			start := time.Now()
			_, e := (PiRunner{Config: cfg}).Run(context.Background(), Input{Version: 2})
			if (mode == "success" || mode == "launcher") && e != nil {
				t.Fatal(e)
			}
			if mode != "success" && mode != "launcher" && e == nil {
				t.Fatal("limit not enforced")
			}
			if mode == "timeout" && time.Since(start) > 5*time.Second {
				t.Fatal("timeout not bounded")
			}
			if e != nil && strings.Contains(e.Error(), "fake-bearer") {
				t.Fatal("credential leaked")
			}
			var unchanged map[string]any
			if e := durable.Read(filepath.Join(authDir, "settings.json"), &unchanged); e != nil {
				t.Fatal(e)
			}
			if unchanged["extensions"] == nil {
				t.Fatal("changed installed config")
			}
		})
	}
}
