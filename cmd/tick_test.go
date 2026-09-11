package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTickPollsGitHubUsingConfiguredPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh uses a POSIX script")
	}
	home := t.TempDir()
	if out, err := runCLI(t, home, "human:cli", "init"); err != nil {
		t.Fatal(out, err)
	}
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(map[string]any{"version": 2, "github_poll_seconds": 300, "runner": map[string]any{"path": bin}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "config.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, home, "human:cli", "tick")
	if err != nil || !strings.Contains(out, `"attempted": true`) || !strings.Contains(out, "baseline snapshot") {
		t.Fatal(out, err)
	}
	out, err = runCLI(t, home, "human:cli", "tick")
	if err != nil || !strings.Contains(out, `"attempted": false`) {
		t.Fatal(out, err)
	}
	// No narrative is invented by a scheduled poll.
	out, err = runCLI(t, home, "human:cli", "today", "--json")
	if err != nil || !strings.Contains(out, `"entries": []`) || !strings.Contains(out, `"prs_fetched_at"`) {
		t.Fatal(out, err)
	}
}
