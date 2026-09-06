package cmd

import (
	"context"
	"fmt"
	"github.com/drdreo/daylog/internal/athena"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func health() (map[string]any, error) {
	cfg, e := config.Load()
	if e != nil {
		return nil, e
	}
	q, e := capture.Open()
	if e != nil {
		return nil, e
	}
	items, e := q.List("")
	if e != nil {
		return nil, e
	}
	counts := map[string]int{"pending": 0, "processing": 0, "processed": 0, "error": 0, "skip": 0, "hold": 0}
	last := ""
	oldest := ""
	failures := []map[string]string{}
	coverage := map[string]string{"pi": "not configured", "claude": "not configured", "codex": "not configured"}
	for _, s := range cfg.CaptureScopes {
		coverage[s.Harness] = "approved scope configured; unsaved-session gaps remain"
	}
	for _, it := range items {
		counts[it.Receipt.Status]++
		if it.Receipt.Disposition == "skip" || it.Receipt.Disposition == "hold" {
			counts[it.Receipt.Disposition]++
		}
		last = it.Candidate.CapturedAt
		if oldest == "" && (it.Receipt.Status == "pending" || it.Receipt.Status == "processing" || it.Receipt.Status == "error") {
			oldest = it.Candidate.CapturedAt
		}
		if it.Receipt.Status == "error" {
			failures = append(failures, map[string]string{"candidate": it.Candidate.ID, "reason": it.Receipt.Reason})
		}
	}
	temps := []string{}
	if err := filepath.WalkDir(q.Root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if strings.HasPrefix(d.Name(), ".tmp-") {
			temps = append(temps, p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sourceHealth := []map[string]any{}
	paths, err := filepath.Glob(filepath.Join(q.Root, "cursors", "*.json"))
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		var raw map[string]any
		if err := durable.Read(path, &raw); err != nil {
			return nil, err
		}
		if raw["error"] == nil && raw["last_observed_at"] == nil {
			continue
		}
		sourceHealth = append(sourceHealth, map[string]any{"source": raw["harness"], "adapter": raw["adapter"], "observed_at": raw["observed_at"], "last_scan": raw["last_observed_at"], "path": raw["path"], "error": raw["error"]})
	}
	age := 0.0
	if oldest != "" {
		t, _ := time.Parse(time.RFC3339Nano, oldest)
		age = time.Since(t).Seconds()
	}
	return map[string]any{"build": store.BuildVersion, "store_version": store.Version, "policy": athena.PolicyVersion, "mode": cfg.Mode, "queue": counts, "last_observed_input": last, "oldest_unfinished": oldest, "queue_age_seconds": age, "failures": failures, "interrupted_temporary_files": temps, "coverage": coverage, "source_health": sourceHealth, "runner": cfg.Runner}, nil
}
func init() {
	var jsonOut bool
	s := &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		h, e := health()
		if e != nil {
			return e
		}
		return printJSON(cmd, h)
	}}
	s.Flags().BoolVar(&jsonOut, "json", false, "machine-readable status (always JSON)")
	rootCmd.AddCommand(s)
	var deep bool
	d := &cobra.Command{Use: "doctor", Short: "Check store, queue, and Athena's configured pi without invoking the model", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		h, e := health()
		if e != nil {
			return e
		}
		issues := []string{}
		if _, e := store.ReadAll(); e != nil {
			issues = append(issues, e.Error())
		}
		cfg, e := config.Load()
		if e != nil {
			return e
		}
		if cfg.Runner.Binary == "" {
			issues = append(issues, "runner.binary not configured")
		} else if _, e := os.Stat(cfg.Runner.Binary); e != nil {
			issues = append(issues, "runner binary missing")
		}
		if _, e := os.Stat(filepath.Join(cfg.Runner.AgentDir, "auth.json")); e != nil {
			issues = append(issues, "pi auth.json absent; provider may require login")
		}
		if len(cfg.CloudProjects) == 0 {
			issues = append(issues, "no cloud projects approved; local intake remains available")
		}
		if deep && cfg.Runner.Binary != "" {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			args := []string{"--offline", "--no-extensions", "--no-skills", "--no-context-files", "--no-prompt-templates", "--no-themes", "--no-approve", "--list-models", cfg.Runner.Model}
			p := exec.CommandContext(ctx, cfg.Runner.Binary, append(append([]string{}, cfg.Runner.Arguments...), args...)...)
			p.Env = append(os.Environ(), "PATH="+cfg.Runner.Path, "PI_CODING_AGENT_DIR="+cfg.Runner.AgentDir, "DAYLOG_INTERNAL=1")
			b, e := p.Output()
			if e != nil || !strings.Contains(string(b), cfg.Runner.Model) {
				issues = append(issues, "Athena's configured model was not found in the existing pi catalog; reports remain queued")
			}
			h["model_catalog"] = string(b)
		}
		h["issues"] = issues
		if e := printJSON(cmd, h); e != nil {
			return e
		}
		if len(issues) > 0 {
			return fmt.Errorf("doctor found %d configuration/health issue(s)", len(issues))
		}
		return nil
	}}
	d.Flags().BoolVar(&deep, "check-model", false, "query installed pi catalog (no inference)")
	rootCmd.AddCommand(d)
}
