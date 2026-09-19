package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/athena"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

func diagnosticFixture(t *testing.T) (string, *capture.Spool, config.Config) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DAYLOG_DIR", filepath.Join(home, "data"))
	q, err := capture.Open()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	// This executable must never run: diagnostics here do not check the catalog.
	cfg.Runner.Binary = testBinary
	cfg.Runner.AgentDir = filepath.Join(home, "pi")
	if err := durable.JSON(filepath.Join(cfg.Runner.AgentDir, "auth.json"), map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAll(); err != nil {
		t.Fatal(err)
	}
	return home, q, cfg
}

func diagnosticCandidate(t *testing.T, q *capture.Spool, at time.Time, text string) capture.Candidate {
	t.Helper()
	c, err := q.Enqueue(capture.Candidate{Version: capture.Version, Source: "agent:pi", Kind: "work", Text: text, CapturedAt: at.Format(time.RFC3339Nano), OccurredAt: at.Add(-24 * time.Hour).Format(time.RFC3339Nano), TimeBasis: "report", Origin: "report", Completeness: "claim", Terminal: "completed", Context: event.Context{Cwd: filepath.Join(filepath.Dir(q.Root), "..", "synthetic-project")}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func diagnosticJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var h map[string]any
	// Cobra appends a nonzero-exit diagnostic after doctor's JSON when unhealthy.
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&h); err != nil {
		t.Fatal(out, err)
	}
	return h
}

func diagnosticFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		files[path] = string(b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestDiagnosticsStaleSnapshotAndMissingScopes(t *testing.T) {
	home, q, _ := diagnosticFixture(t)
	at := time.Now().Add(-48 * time.Hour)
	c := diagnosticCandidate(t, q, at, "Implemented a synthetic fix; deployment not attempted.")
	if err := durable.JSON(filepath.Join(q.Root, "cursors", "synthetic.json"), map[string]any{"harness": "pi", "last_observed_at": at.Add(-time.Hour).Format(time.RFC3339Nano), "error": "unsupported synthetic session version"}); err != nil {
		t.Fatal(err)
	}
	before := diagnosticFiles(t, filepath.Join(home, "data"))
	out, err := runCLI(t, home, "human:cli", "status", "--json")
	if err != nil {
		t.Fatal(out, err)
	}
	h := diagnosticJSON(t, out)
	if h["last_observed_input"] != c.CapturedAt || h["oldest_unfinished"] != c.CapturedAt || h["queue_age_seconds"].(float64) < 47*3600 {
		t.Fatal("capture freshness was confused with occurrence or a cursor observation", h)
	}
	for _, value := range h["coverage"].(map[string]any) {
		if !strings.Contains(value.(string), "not configured") || !strings.Contains(value.(string), "explicit reports can still enqueue") {
			t.Fatal(value)
		}
	}
	if !strings.Contains(out, "unsupported synthetic session version") || !strings.Contains(out, "last_scan") {
		t.Fatal("saved source gap was hidden", out)
	}
	wantKeys := []string{"build", "store_version", "policy", "mode", "queue", "last_observed_input", "oldest_unfinished", "queue_age_seconds", "failures", "interrupted_temporary_files", "coverage", "source_health", "runner"}
	if len(h) != len(wantKeys) {
		t.Fatal("status schema changed", h)
	}
	for _, key := range wantKeys {
		if _, ok := h[key]; !ok {
			t.Fatal("missing existing key", key)
		}
	}
	out, err = runCLI(t, home, "human:cli", "doctor")
	if err == nil || !strings.Contains(out, "unfinished report(s) captured longer ago") || !strings.Contains(out, "does not prove the worker is stopped") {
		t.Fatal(out, err)
	}
	if after := diagnosticFiles(t, filepath.Join(home, "data")); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostics modified saved state")
	}
}

func TestDiagnosticsQueueStatesAndDispositionsRemainIndependent(t *testing.T) {
	home, q, _ := diagnosticFixture(t)
	for _, disposition := range []string{"hold", "skip", "outcome"} {
		c := diagnosticCandidate(t, q, time.Now(), "Synthetic editorial disposition fixture.")
		if err := q.SaveReceipt(capture.Receipt{Version: capture.Version, CandidateID: c.ID, Status: "processed", Disposition: disposition, UpdatedAt: c.CapturedAt}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runCLI(t, home, "human:cli", "status")
	if err != nil {
		t.Fatal(out, err)
	}
	counts := diagnosticJSON(t, out)["queue"].(map[string]any)
	if counts["processed"] != float64(3) || counts["hold"] != float64(1) || counts["skip"] != float64(1) || counts["pending"] != float64(0) || counts["error"] != float64(0) {
		t.Fatal(counts)
	}
	out, err = runCLI(t, home, "human:cli", "doctor")
	if err != nil {
		t.Fatal("processed dispositions are not blocked work", out, err)
	}
}

func TestDiagnosticsConfiguredScopeDoesNotImplyCoverage(t *testing.T) {
	home, _, cfg := diagnosticFixture(t)
	cfg.CaptureScopes = []config.Scope{{Harness: "pi", Directory: filepath.Join(home, "missing-sessions"), Projects: []string{filepath.Join(home, "synthetic-project")}}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, home, "human:cli", "status")
	if err != nil || !strings.Contains(out, "configured, not verified coverage") {
		t.Fatal(out, err)
	}
	if _, err := os.Stat(cfg.CaptureScopes[0].Directory); !os.IsNotExist(err) {
		t.Fatal("diagnostic created a transcript scope", err)
	}
}

func TestDiagnosticsPolicyMismatchAndSavedTimes(t *testing.T) {
	home, q, cfg := diagnosticFixture(t)
	now := time.Now()
	c := diagnosticCandidate(t, q, now.Add(-time.Hour), "Completed a synthetic parser change.")
	p := athena.Plan{Version: athena.Version, ID: "synthetic-plan", Mode: "live", Status: "ready", CreatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), Input: athena.Input{Version: athena.Version, Policy: "synthetic-old-policy", Candidates: []capture.Candidate{c}}}
	p.InputHash = capture.JSONHash(p.Input)
	if err := durable.JSON(filepath.Join(q.Root, "plans", p.ID+".json"), p); err != nil {
		t.Fatal(err)
	}
	r := capture.Receipt{Version: capture.Version, CandidateID: c.ID, Status: "processing", UpdatedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano), PlanID: p.ID, Policy: p.Input.Policy, Attempts: 1}
	if err := q.SaveReceipt(r); err != nil {
		t.Fatal(err)
	}
	before := diagnosticFiles(t, filepath.Join(home, "data"))
	out, err := runCLI(t, home, "human:cli", "doctor")
	if err == nil || !strings.Contains(out, "saved policy different from running policy "+athena.PolicyVersion) {
		t.Fatal(out, err)
	}
	out, err = runCLI(t, home, "human:cli", "explain", c.ID)
	if err != nil {
		t.Fatal(out, err)
	}
	h := diagnosticJSON(t, out)
	if h["candidate"].(map[string]any)["captured_at"] != c.CapturedAt || h["receipt"].(map[string]any)["updated_at"] != r.UpdatedAt || h["plan"].(map[string]any)["created_at"] != p.CreatedAt {
		t.Fatal("explain lost distinct saved timestamps", h)
	}
	out, err = runCLI(t, home, "human:cli", "version")
	if err != nil || diagnosticJSON(t, out)["policy"] != athena.PolicyVersion {
		t.Fatal(out, err)
	}
	if after := diagnosticFiles(t, filepath.Join(home, "data")); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostic applied, stopped, or rewrote the saved plan")
	}
	// Historical policy differences after application are not live-plan blockers.
	p.Status = "applied"
	if err := durable.JSON(filepath.Join(q.Root, "plans", p.ID+".json"), p); err != nil {
		t.Fatal(err)
	}
	issues, err := diagnoseQueue(q, cfg, nil, now)
	if err != nil || strings.Contains(strings.Join(issues, "\n"), "saved policy different") {
		t.Fatal(issues, err)
	}
}

type diagnosticRunner struct{ calls int }

func (r *diagnosticRunner) Run(context.Context, athena.Input) (athena.Output, error) {
	r.calls++
	return athena.Output{}, fmt.Errorf("pi timeout/cancellation: synthetic timeout")
}

func TestDiagnosticsBlockedProcessingDistinguishesBudgetInputAndRunner(t *testing.T) {
	for _, kind := range []string{"budget", "input", "runner"} {
		t.Run(kind, func(t *testing.T) {
			home, q, cfg := diagnosticFixture(t)
			now := time.Now()
			c := diagnosticCandidate(t, q, now.Add(-time.Hour), "A bounded synthetic report.")
			cfg.Runner.MaxAttempts = 1
			if kind == "input" {
				// Valid local candidate whose referenced evidence has disappeared.
				c.Evidence = []string{"e-missing-synthetic"}
				if err := durable.JSON(filepath.Join(q.Root, "candidates", c.ID+".json"), c); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "budget" {
				if err := durable.JSON(filepath.Join(q.Root, "budget.json"), athena.Budget{Version: athena.Version, Date: now.Format("2006-01-02"), Calls: cfg.Runner.CallsPerDay}); err != nil {
					t.Fatal(err)
				}
			}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			runner := &diagnosticRunner{}
			worker := athena.Worker{Queue: q, Config: cfg, Runner: runner, Now: func() time.Time { return now }}
			if _, err := worker.Once(context.Background(), false); err == nil {
				t.Fatal("expected synthetic failure")
			}
			wantCalls := 0
			if kind == "runner" {
				wantCalls = 1
			}
			if runner.calls != wantCalls {
				t.Fatalf("%s invoked runner %d times, want %d", kind, runner.calls, wantCalls)
			}
			before := diagnosticFiles(t, filepath.Join(home, "data"))
			out, err := runCLI(t, home, "human:cli", "doctor")
			if err == nil {
				t.Fatal("blocked queue reported healthy", out)
			}
			want := map[string]string{"budget": "daily model call budget exhausted", "input": "missing or unreadable local evidence", "runner": "runner/model failure"}[kind]
			if !strings.Contains(out, want) || (kind != "budget" && !strings.Contains(out, "automatic retries exhausted")) {
				t.Fatal(out)
			}
			if kind != "budget" && strings.Contains(out, "daily model call budget exhausted") {
				t.Fatal("failure misreported as exhausted budget", out)
			}
			if after := diagnosticFiles(t, filepath.Join(home, "data")); !reflect.DeepEqual(before, after) {
				t.Fatal("doctor changed blocked queue")
			}
		})
	}
}

func TestDiagnosticsOldBudgetIsNotExhaustedToday(t *testing.T) {
	_, q, cfg := diagnosticFixture(t)
	now := time.Now()
	budget := athena.Budget{Version: athena.Version, Date: now.AddDate(0, 0, -1).Format("2006-01-02"), Calls: cfg.Runner.CallsPerDay}
	path := filepath.Join(q.Root, "budget.json")
	if err := durable.JSON(path, budget); err != nil {
		t.Fatal(err)
	}
	issues, err := diagnoseQueue(q, cfg, nil, now)
	if err != nil || len(issues) != 0 {
		t.Fatal(issues, err)
	}
	var after athena.Budget
	if err := durable.Read(path, &after); err != nil || after != budget {
		t.Fatal("diagnostics reset a stale budget", after, err)
	}
}

func TestDiagnosticsCompleteRedactionMarkers(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"", false}, {" \r\n", false}, {"[excerpt truncated]", false},
		{"[sensitive line excluded]", true},
		{"[sensitive line excluded]\r\n\n[sensitive line excluded]\n[excerpt truncated]", true},
		{"[sensitive line excluded]\nRetained synthetic result", false},
		{"[sensitive line excluded]\n[sensitive line excl\n[excerpt truncated]", false},
		{"[excerpt truncated]\n[sensitive line excluded]", false},
	} {
		if got := completelyRedacted(tc.text); got != tc.want {
			t.Errorf("completelyRedacted(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
	home, q, _ := diagnosticFixture(t)
	diagnosticCandidate(t, q, time.Now(), "[sensitive line excluded]\n[sensitive line excluded]")
	out, err := runCLI(t, home, "human:cli", "doctor")
	if err == nil || !strings.Contains(out, "1 unfinished report(s) have marker-only") || !strings.Contains(out, "original content and work coverage unknown") {
		t.Fatal(out, err)
	}
}

func TestDiagnosticsRedactedEvidenceVersusRetainedProse(t *testing.T) {
	for _, tc := range []struct {
		text string
		flag bool
	}{
		{"[sensitive line excluded]\n[excerpt truncated]", true},
		{"[sensitive line excluded]\nRetained synthetic result.", false},
	} {
		t.Run(fmt.Sprint(tc.flag), func(t *testing.T) {
			home, q, _ := diagnosticFixture(t)
			c := diagnosticCandidate(t, q, time.Now(), "Native boundary with separately retained evidence.")
			e, err := q.PutEvidence(tc.text, "claim", c.CapturedAt)
			if err != nil {
				t.Fatal(err)
			}
			c.Evidence = []string{e.ID}
			if err := durable.JSON(filepath.Join(q.Root, "candidates", c.ID+".json"), c); err != nil {
				t.Fatal(err)
			}
			before := diagnosticFiles(t, filepath.Join(home, "data"))
			out, err := runCLI(t, home, "human:cli", "doctor")
			if (err != nil) != tc.flag || strings.Contains(out, "1 unfinished report(s) have marker-only") != tc.flag {
				t.Fatal(out, err)
			}
			if after := diagnosticFiles(t, filepath.Join(home, "data")); !reflect.DeepEqual(before, after) {
				t.Fatal("diagnostic changed evidence")
			}
		})
	}
}

func TestDiagnosticFailureClassification(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{
		{"candidate synthetic-id cannot fit without outcome context: Athena input exceeds cap (1200 bytes, limit 1024)", "local input failure (candidate cannot fit)"},
		{"current report batch cannot fit without outcome context: Athena input exceeds cap (1200 bytes, limit 1024)", "local input failure (report batch cannot fit)"},
		{"required linked outcome context cannot fit: Athena input exceeds cap (1200 bytes, limit 1024); reports were not truncated", "local input failure (required linked context cannot fit)"},
		{"required linked outcome context exceeds 64 outcomes; reports were not truncated", "local input failure (required linked context cannot fit)"},
		{"Athena input exceeds cap", "local input failure (before model invocation)"},
		{"invalid Athena decision JSON: synthetic error", "processing failure (not necessarily a provider failure)"},
		{"candidate not accounted for", "processing failure (not necessarily a provider failure)"},
		{"Athena's configured model synthetic/model is unavailable or failed: synthetic error", "runner/model failure (local execution or provider; provider cause not confirmed)"},
	} {
		if got := failureClass(tc.reason); got != tc.want {
			t.Errorf("failureClass(%q) = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

func TestDiagnosticFailureReasonsAreBoundedAndDoNotMutateReceipts(t *testing.T) {
	cfg := config.Defaults()
	now := time.Now()
	for _, tc := range []struct {
		r    capture.Receipt
		want string
	}{
		{capture.Receipt{Reason: "Athena input exceeds cap", Attempts: cfg.Runner.MaxAttempts}, "automatic retries exhausted"},
		{capture.Receipt{PlanID: "synthetic-plan", Attempts: cfg.Runner.MaxAttempts}, "retained plan"},
		{capture.Receipt{NextAttemptAt: now.Add(time.Hour).Format(time.RFC3339Nano)}, "backoff until"},
		{capture.Receipt{}, "retry allowance remains"},
	} {
		before := tc.r
		if got := diagnosticFailure(tc.r, cfg, now); !strings.Contains(got, tc.want) {
			t.Fatal(got)
		}
		if !reflect.DeepEqual(tc.r, before) {
			t.Fatal("diagnostic mutated a receipt")
		}
	}
	r := capture.Receipt{Reason: "token=synthetic-secret\n" + strings.Repeat("safe ", 1000)}
	got := diagnosticFailure(r, cfg, now)
	if strings.Contains(got, "synthetic-secret") || len(got) > 1400 {
		t.Fatal("diagnostic leaked or exceeded its bound", got)
	}
}
