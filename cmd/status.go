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
	coverage := map[string]string{}
	for _, harness := range []string{"pi", "claude", "codex"} {
		coverage[harness] = "optional native recovery not configured; explicit reports can still enqueue; no transcript coverage implied"
	}
	for _, s := range cfg.CaptureScopes {
		coverage[s.Harness] = "optional native recovery scope configured, not verified coverage; inspect source_health scan times/errors; unsupported and unsaved sessions remain gaps"
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
			failures = append(failures, map[string]string{"candidate": it.Candidate.ID, "reason": diagnosticFailure(it.Receipt, cfg, time.Now())})
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
		age = max(0, time.Since(t).Seconds())
	}
	return map[string]any{"build": store.BuildVersion, "store_version": store.Version, "policy": athena.PolicyVersion, "mode": cfg.Mode, "queue": counts, "last_observed_input": last, "oldest_unfinished": oldest, "queue_age_seconds": age, "failures": failures, "interrupted_temporary_files": temps, "coverage": coverage, "source_health": sourceHealth, "runner": cfg.Runner}, nil
}

// Diagnostic annotations are read-time explanations, never receipt mutations.
func diagnosticFailure(r capture.Receipt, cfg config.Config, now time.Time) string {
	retry := "retry allowance remains, subject to mode, budget, and input checks"
	switch {
	case r.PlanID != "":
		retry = "retained plan; inspect explain before an explicit human retry"
	case r.Attempts >= cfg.Runner.MaxAttempts:
		retry = "automatic retries exhausted; inspect explain before an explicit human retry"
	default:
		if at, err := time.Parse(time.RFC3339Nano, r.NextAttemptAt); err == nil && now.Before(at) {
			retry = "backoff until " + r.NextAttemptAt
		}
	}
	return fmt.Sprintf("%s; attempts %d/%d; %s; recorded reason: %s", failureClass(r.Reason), r.Attempts, cfg.Runner.MaxAttempts, retry, capture.Redact(r.Reason, 1024))
}

func failureClass(reason string) string {
	switch {
	case strings.HasPrefix(reason, "candidate ") && strings.Contains(reason, " cannot fit without outcome context: Athena input exceeds cap ("):
		return "local input failure (candidate cannot fit)"
	case strings.HasPrefix(reason, "current report batch cannot fit without outcome context: Athena input exceeds cap ("):
		return "local input failure (report batch cannot fit)"
	case strings.HasPrefix(reason, "required linked outcome context cannot fit: Athena input exceeds cap ("), reason == "required linked outcome context exceeds 64 outcomes; reports were not truncated":
		return "local input failure (required linked context cannot fit)"
	case reason == "Athena input exceeds cap", reason == "reports from the daylog data directory cannot be curated", reason == "related outcome context exceeds cap; narrow task linkage":
		return "local input failure (before model invocation)"
	case strings.HasPrefix(reason, "Athena unavailable:"):
		return "local runner setup failure"
	case strings.HasPrefix(reason, "Athena's configured model "), strings.HasPrefix(reason, "pi timeout/cancellation:"):
		return "runner/model failure (local execution or provider; provider cause not confirmed)"
	default:
		return "processing failure (not necessarily a provider failure)"
	}
}

// Only the existing redaction markers count. Empty input and retained prose do
// not prove complete redaction; a truncation marker alone does not either.
func completelyRedacted(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	seen := false
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case "":
		case "[sensitive line excluded]":
			seen = true
		case "[excerpt truncated]":
			if i != len(lines)-1 {
				return false
			}
		default:
			return false
		}
	}
	return seen
}

func diagnoseQueue(q *capture.Spool, cfg config.Config, items []capture.Item, now time.Time) ([]string, error) {
	issues := []string{}
	if cfg.Mode == "shadow" {
		issues = append(issues, "processing paused by legacy shadow configuration; capture is not publication")
	}
	var exhausted, retained, aging, redacted, missingEvidence int
	classes := map[string]int{}
	for _, it := range items {
		r := it.Receipt
		if r.Status == "error" {
			classes[failureClass(r.Reason)]++
			if r.PlanID != "" {
				retained++
			} else if r.Attempts >= cfg.Runner.MaxAttempts {
				exhausted++
			}
		}
		if r.Status == "processed" {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, it.Candidate.CapturedAt)
		if now.Sub(at) > time.Duration(cfg.MaxWaitSeconds+cfg.Runner.TimeoutSeconds)*time.Second {
			aging++
		}
		markerOnly := completelyRedacted(capture.Redact(it.Candidate.Text, capture.MaxReportBytes))
		missing := false
		for _, id := range it.Candidate.Evidence {
			e, err := q.GetEvidence(id)
			if err != nil {
				missing = true
				continue
			}
			markerOnly = markerOnly || completelyRedacted(e.Text)
		}
		if markerOnly {
			redacted++
		}
		if missing {
			missingEvidence++
		}
	}
	// Stable, bounded summaries rather than copying report/evidence text.
	for _, class := range []string{
		"local input failure (candidate cannot fit)",
		"local input failure (report batch cannot fit)",
		"local input failure (required linked context cannot fit)",
		"local input failure (before model invocation)",
		"local runner setup failure",
		"runner/model failure (local execution or provider; provider cause not confirmed)",
		"processing failure (not necessarily a provider failure)",
	} {
		if n := classes[class]; n > 0 {
			issues = append(issues, fmt.Sprintf("%d report(s): %s; inspect status failures and explain", n, class))
		}
	}
	if exhausted > 0 {
		issues = append(issues, fmt.Sprintf("%d report(s): automatic retries exhausted; no automatic reset or replay", exhausted))
	}
	if retained > 0 {
		issues = append(issues, fmt.Sprintf("%d failed report(s) retain a saved plan; automatic fresh evaluation is blocked, inspect explain", retained))
	}
	if aging > 0 {
		issues = append(issues, fmt.Sprintf("%d unfinished report(s) captured longer ago than batching max wait plus runner timeout; this snapshot does not prove the worker is stopped", aging))
	}
	if redacted > 0 {
		issues = append(issues, fmt.Sprintf("%d unfinished report(s) have marker-only report text or evidence: only redaction markers remain in the retained excerpt (possibly truncated); original content and work coverage unknown, not empty input or a provider failure", redacted))
	}
	if missingEvidence > 0 {
		issues = append(issues, fmt.Sprintf("%d unfinished report(s) have missing or unreadable local evidence; model input is blocked before provider invocation", missingEvidence))
	}
	budget := athena.Budget{}
	if err := durable.Read(filepath.Join(q.Root, "budget.json"), &budget); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else if budget.Version != athena.Version {
		issues = append(issues, "unsupported local call budget version; processing is blocked before provider invocation")
	} else if budget.Date == now.Format("2006-01-02") && budget.Calls >= cfg.Runner.CallsPerDay {
		issues = append(issues, fmt.Sprintf("daily model call budget exhausted (%d/%d, %s); includes dry runs, not a provider failure; intake continues", budget.Calls, cfg.Runner.CallsPerDay, budget.Date))
	}
	paths, err := filepath.Glob(filepath.Join(q.Root, "plans", "*.json"))
	if err != nil {
		return nil, err
	}
	var mismatch, shadow int
	for _, path := range paths {
		var plan athena.Plan
		if err := durable.Read(path, &plan); err != nil {
			return nil, err
		}
		if plan.Status == "ready" && plan.Mode == "live" && plan.Input.Policy != athena.PolicyVersion {
			mismatch++
		}
		if plan.Status == "shadow" || (plan.Status == "ready" && plan.Mode == "shadow") {
			shadow++
		}
	}
	if mismatch > 0 {
		issues = append(issues, fmt.Sprintf("%d ready live plan(s) have a saved policy different from running policy %s; they cannot apply under this binary; inspect explain, no automatic re-evaluation", mismatch, athena.PolicyVersion))
	}
	if shadow > 0 {
		issues = append(issues, fmt.Sprintf("%d saved shadow plan(s) will never auto-publish; upgrading or switching mode does not re-evaluate them", shadow))
	}
	return issues, nil
}

func init() {
	var jsonOut bool
	s := &cobra.Command{Use: "status", Short: "Inspect capture and queue state, not proof of publication or complete coverage", Long: `Inspect the current capture and queue snapshot (always JSON).
Queue pending/processing/processed/error are processing states; hold/skip are
independent editorial dispositions, usually on processed items. last_observed_input is capture
time, not processing, publication, or the journal display day. source_health
contains saved observations, not a live scan. Run doctor for blocked-state
summaries and explain CANDIDATE for receipt and saved-plan details.`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
		q, e := capture.Open()
		if e != nil {
			return e
		}
		items, e := q.List("")
		if e != nil {
			return e
		}
		queueIssues, e := diagnoseQueue(q, cfg, items, time.Now())
		if e != nil {
			return e
		}
		issues = append(issues, queueIssues...)
		if cfg.Runner.Binary == "" {
			issues = append(issues, "local runner setup: runner.binary not configured; this is not a provider failure")
		} else if _, e := os.Stat(cfg.Runner.Binary); e != nil {
			issues = append(issues, "local runner setup: runner binary missing; this is not a provider failure")
		}
		if _, e := os.Stat(filepath.Join(cfg.Runner.AgentDir, "auth.json")); e != nil {
			issues = append(issues, "pi auth.json absent; provider may require login")
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
