package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveDefaultAndExplicitDryRun(t *testing.T) {
	home := t.TempDir()
	if out, err := runCLI(t, home, "human:cli", "init"); err != nil {
		t.Fatal(out, err)
	}
	path := filepath.Join(home, "data", "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(before, &cfg); err != nil || cfg.Mode != "live" {
		t.Fatal("new installations must not silently start in shadow", cfg, err)
	}
	out, err := runCLI(t, home, "human:cli", "curate", "--once", "--dry-run")
	if err != nil || !strings.Contains(out, `"mode": "dry-run"`) {
		t.Fatal(out, err)
	}
	out, err = runCLI(t, home, "human:cli", "setup", "--mode", "shadow")
	if err == nil || !strings.Contains(out, "--dry-run") {
		t.Fatal("setup should direct preview users to the explicit flag", out, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("preview or rejected setup changed machine configuration", err)
	}
}
