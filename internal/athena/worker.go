package athena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	original "github.com/drdreo/daylog/internal/context"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Worker struct {
	Queue       *capture.Spool
	Config      config.Config
	Runner      Runner
	Now         func() time.Time
	AfterAppend func() error
}
type Result struct {
	Mode    string   `json:"mode"`
	Calls   int      `json:"calls"`
	Plans   int      `json:"plans"`
	Events  int      `json:"events"`
	Waiting int      `json:"waiting"`
	Preview []Action `json:"preview,omitempty"`
}
type Budget struct {
	Version int    `json:"version"`
	Date    string `json:"date"`
	Calls   int    `json:"calls"`
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
func (w *Worker) planPath(id string) string { return filepath.Join(w.Queue.Root, "plans", id+".json") }
func (w *Worker) Once(ctx context.Context, dryRun bool) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(w.Config.Runner.TimeoutSeconds)*time.Second)
	defer cancel()
	res := Result{Mode: "live"}
	if dryRun {
		res.Mode = "dry-run"
	} else if w.Config.Mode == "shadow" {
		return res, fmt.Errorf("legacy shadow configuration is paused; run daylog setup --mode live to resume, or curate --once --dry-run to preview")
	}
	unlock, err := durable.Lock(filepath.Join(w.Queue.Root, "worker.lock"), false)
	if err != nil {
		return res, err
	}
	defer unlock()
	var runErr error
	// Preview never recovers or acknowledges saved plans. Only live runs mutate
	// receipts and publications. Legacy shadow plans remain permanently inert.
	if !dryRun {
		paths, _ := filepath.Glob(filepath.Join(w.Queue.Root, "plans", "*.json"))
		for _, p := range paths {
			var plan Plan
			if err := durable.Read(p, &plan); err != nil {
				return res, err
			}
			if plan.Version != Version {
				return res, fmt.Errorf("unsupported saved plan")
			}
			if plan.Status == "ready" && plan.Mode == "shadow" {
				plan.Status = "shadow"
				if err := durable.JSON(p, plan); err != nil {
					return res, err
				}
			}
			if plan.Status == "applied" || plan.Status == "shadow" || plan.Status == "stale" {
				if err := w.ack(plan); err != nil {
					return res, err
				}
			}
			if plan.Status == "ready" && plan.Mode == "live" {
				n, e := w.apply(&plan)
				res.Events += n
				if e != nil {
					if plan.Status != "stale" {
						return res, e
					}
					runErr = errors.Join(runErr, e)
				}
			}
		}
		items, err := w.Queue.List("")
		if err != nil {
			return res, err
		}
		for i := range items {
			r := &items[i].Receipt
			if r.Status == "processing" {
				if r.PlanID != "" {
					var p Plan
					if err := durable.Read(w.planPath(r.PlanID), &p); err == nil {
						if p.Status == "ready" {
							continue
						}
						if err := w.ack(p); err != nil {
							return res, err
						}
						continue
					} else if !os.IsNotExist(err) {
						return res, err
					}
				}
				r.Status = "pending"
				r.Reason = "reclaimed interrupted worker"
				if err := w.Queue.SaveReceipt(*r); err != nil {
					return res, err
				}
			}
		}
	}
	items, err := w.Queue.List("")
	if err != nil {
		return res, err
	}
	groups := map[string][]capture.Item{}
	order := []string{}
	now := w.now()
	for _, it := range items {
		r := it.Receipt
		eligible := r.Status == "pending"
		if r.Status == "error" && r.PlanID == "" && r.Attempts < w.Config.Runner.MaxAttempts {
			at, _ := time.Parse(time.RFC3339Nano, r.NextAttemptAt)
			eligible = !now.Before(at)
		}
		if !eligible {
			continue
		}
		k := editorialGroup(it.Candidate)
		if len(groups[k]) == 0 {
			order = append(order, k)
		}
		groups[k] = append(groups[k], it)
	}
	for _, groupKey := range order {
		if err := ctx.Err(); err != nil {
			return res, errors.Join(runErr, err)
		}
		if res.Calls >= w.Config.Runner.CallsPerRun {
			break
		}
		group := groups[groupKey]
		first, _ := time.Parse(time.RFC3339Nano, group[0].Candidate.CapturedAt)
		last, _ := time.Parse(time.RFC3339Nano, group[len(group)-1].Candidate.CapturedAt)
		if now.Sub(last) < time.Duration(w.Config.QuietSeconds)*time.Second && now.Sub(first) < time.Duration(w.Config.MaxWaitSeconds)*time.Second {
			res.Waiting += len(group)
			continue
		}
		// Preflight each report independently so one unfit report cannot poison
		// its episode or spend another episode's invocation allowance. Reports
		// that fit alone but not together stay pending for a later bounded batch.
		// Bound examined reports too: unfit tails must not consume the entire
		// worker deadline before an already-selected batch can reach the runner.
		selected := []capture.Item{}
		for scanned, it := range group {
			if scanned >= w.Config.BatchSize {
				break
			}
			if err := ctx.Err(); err != nil {
				return res, errors.Join(runErr, err)
			}
			if _, err := w.input([]capture.Item{it}); err != nil {
				if !dryRun {
					if e := w.fail([]capture.Item{it}, err); e != nil {
						return res, e
					}
				}
				runErr = errors.Join(runErr, err)
				continue
			}
			trial := append(append([]capture.Item{}, selected...), it)
			if _, err := w.input(trial); err != nil {
				var limit *inputLimitError
				if !errors.As(err, &limit) {
					return res, errors.Join(runErr, err)
				}
				// Finalize this fitting batch instead of searching the whole
				// episode for a smaller report. The rest remain eligible.
				break
			}
			selected = trial
		}
		if len(selected) == 0 {
			continue
		}
		group = append([]capture.Item{}, selected...)
		// Only same-day exact episode arrivals reopen held/skipped evidence.
		for _, old := range items {
			if len(group) >= w.Config.BatchSize {
				break
			}
			if editorialGroup(old.Candidate) == groupKey && old.Receipt.Status == "processed" && (old.Receipt.Disposition == "hold" || old.Receipt.Disposition == "skip") {
				group = append(group, old)
			}
		}
		in, err := w.input(group)
		if err != nil {
			if !dryRun {
				// Do not rewrite already-processed held/skipped receipts on a
				// local preflight failure. Their evidence remains available.
				if e := w.fail(selected, err); e != nil {
					return res, e
				}
			}
			runErr = errors.Join(runErr, err)
			continue
		}
		budgetPath := filepath.Join(w.Queue.Root, "budget.json")
		budget := Budget{Version: Version, Date: now.Format("2006-01-02")}
		if err := durable.Read(budgetPath, &budget); err != nil && !os.IsNotExist(err) {
			return res, err
		}
		if budget.Version != Version {
			return res, fmt.Errorf("unsupported call budget")
		}
		if budget.Date != now.Format("2006-01-02") {
			budget.Date = now.Format("2006-01-02")
			budget.Calls = 0
		}
		if budget.Calls >= w.Config.Runner.CallsPerDay {
			return res, fmt.Errorf("daily model call limit reached; intake continues")
		}
		claimID := ""
		if !dryRun {
			claimID = event.NewID(now)
			for i := range group {
				r := &group[i].Receipt
				r.Status = "processing"
				r.Attempts++
				r.PlanID = claimID
				r.UpdatedAt = now.Format(time.RFC3339Nano)
				if err := w.Queue.SaveReceipt(*r); err != nil {
					return res, err
				}
			}
		}
		// Reserve only after local input validation. Once Runner.Run is
		// attempted, failures (including provider/startup/output failures) are
		// charged and never refunded. Dry runs use the same durable allowance.
		budget.Calls++
		if err := durable.JSON(budgetPath, budget); err != nil {
			return res, err
		}
		res.Calls++
		out, err := w.Runner.Run(ctx, in)
		if err == nil {
			err = Validate(in, out)
		}
		if err != nil {
			if !dryRun {
				if e := w.fail(group, err); e != nil {
					return res, e
				}
			}
			runErr = errors.Join(runErr, err)
			continue
		}
		if dryRun {
			res.Preview = append(res.Preview, out.Actions...)
			continue
		}
		plan := w.makePlan("live", in, out, claimID)
		for _, op := range plan.Operations {
			if op.Event != nil {
				if err := op.Event.Validate(); err != nil {
					return res, err
				}
				current := map[string]event.Entry{}
				for _, e := range in.Outcomes {
					current[e.ID] = e
				}
				if err := event.CheckTargets(*op.Event, current); err != nil {
					return res, err
				}
			}
		}
		// No event can be appended until the entire validated plan is durable.
		if err := durable.JSON(w.planPath(plan.ID), plan); err != nil {
			return res, err
		}
		for _, it := range group {
			r := it.Receipt
			r.PlanID = plan.ID
			r.InputHash = plan.InputHash
			r.Model = plan.Model
			r.Policy = PolicyVersion
			r.Evidence = it.Candidate.Evidence
			if err := w.Queue.SaveReceipt(r); err != nil {
				return res, err
			}
		}
		res.Plans++
		n, err := w.apply(&plan)
		res.Events += n
		if err != nil {
			return res, err
		}
	}
	return res, runErr
}
func (w *Worker) input(group []capture.Item) (Input, error) {
	in := Input{Version: Version, Policy: PolicyVersion, Candidates: []capture.Candidate{}, Evidence: []capture.Evidence{}, Outcomes: []event.Entry{}, EditableTargets: []event.Target{}, Preferences: []Preference{}}
	evidence := map[string]bool{}
	root, _ := store.DataDir()
	for _, it := range group {
		c := it.Candidate
		if it.Receipt.Status == "error" && it.Receipt.Reason != "" {
			in.PreviousErrors = append(in.PreviousErrors, capture.Redact(it.Receipt.Reason, 1024))
		}
		// Queued reports are eligible regardless of project. Keep the data
		// directory excluded so internal Daylog state cannot feed curation.
		if original.Within(c.Context.Cwd, root) {
			return in, fmt.Errorf("reports from the daylog data directory cannot be curated")
		}
		c.Text = capture.Redact(c.Text, capture.MaxReportBytes)
		in.Candidates = append(in.Candidates, c)
		for _, id := range c.Evidence {
			if evidence[id] {
				continue
			}
			e, err := w.Queue.GetEvidence(id)
			if err != nil {
				return in, err
			}
			in.Evidence = append(in.Evidence, e)
			evidence[id] = true
		}
	}
	// Full current reports, evidence, retry feedback and contract fields get
	// space before any history. Never shorten a report to make it fit.
	if err := w.checkInputSize(in); err != nil {
		if len(group) == 1 {
			return in, &inputLimitError{fmt.Sprintf("candidate %s cannot fit without outcome context: %v", group[0].Candidate.ID, err)}
		}
		return in, &inputLimitError{fmt.Sprintf("current report batch cannot fit without outcome context: %v", err)}
	}
	all, err := store.ReadAll()
	if err != nil {
		return in, err
	}
	effective := event.Effective(all)
	required := map[string]bool{}
	distance := map[string]time.Duration{}
	for id, e := range effective {
		for _, c := range in.Candidates {
			// Exact provenance is safety context even if project metadata no
			// longer matches. Omitting it could allow protected-input replay.
			if e.Provenance != nil {
				for _, previous := range e.Provenance.Candidates {
					if previous == c.ID {
						required[id] = true
					}
				}
			}
			if !event.SameProject(e.Context, c.Context) {
				continue
			}
			if c.Episode != "" && e.Provenance != nil && e.Provenance.Episode == c.Episode {
				required[id] = true
			}
			for _, r := range c.Refs {
				for _, er := range e.Refs {
					if r == er {
						required[id] = true
					}
				}
			}
			occurrence, _ := time.Parse(time.RFC3339Nano, c.OccurredAt)
			existing, _ := time.Parse(time.RFC3339Nano, e.DisplayAt)
			delta := occurrence.Sub(existing)
			if delta >= -24*time.Hour && delta <= 7*24*time.Hour {
				if delta < 0 {
					delta = -delta
				}
				if previous, ok := distance[id]; !ok || delta < previous {
					distance[id] = delta
				}
			}
		}
	}
	// Keep the surviving outcome behind a linked merge, including chains.
	for id := range required {
		for next := effective[id].MergedInto; next != "" && !required[next]; next = effective[next].MergedInto {
			if _, ok := effective[next]; !ok {
				break
			}
			required[next] = true
		}
	}
	optional := []event.Entry{}
	for id, e := range effective {
		_, recent := distance[id]
		if !required[id] && !recent {
			continue
		}
		e.TLDR = capture.Redact(e.TLDR, event.MaxTLDRChars*4)
		e.Details = capture.Redact(e.Details, event.MaxDetailsChars*4)
		for i, tag := range e.Tags {
			e.Tags[i] = capture.Redact(tag, event.MaxTagChars*4)
		}
		e.DoneNote = capture.Redact(e.DoneNote, 4096)
		e.Reason = capture.Redact(e.Reason, 1024)
		if required[id] {
			in.Outcomes = append(in.Outcomes, e)
		} else {
			optional = append(optional, e)
		}
	}
	sort.Slice(in.Outcomes, func(i, j int) bool { return in.Outcomes[i].ID < in.Outcomes[j].ID })
	in.EditableTargets = editableTargets(in)
	if len(in.Outcomes) > 64 {
		return in, &inputLimitError{"required linked outcome context exceeds 64 outcomes; reports were not truncated"}
	}
	if err := w.checkInputSize(in); err != nil {
		return in, &inputLimitError{fmt.Sprintf("required linked outcome context cannot fit: %v; reports were not truncated", err)}
	}
	var prefs struct {
		Version  int          `json:"version"`
		Examples []Preference `json:"examples"`
	}
	if err := durable.Read(filepath.Join(w.Queue.Root, "preferences.json"), &prefs); err == nil {
		if prefs.Version != Version {
			return in, fmt.Errorf("unsupported preference version")
		}
		for _, p := range prefs.Examples {
			if e, ok := effective[p.Entry]; ok && event.SameProject(e.Context, group[0].Candidate.Context) {
				p.Reason = capture.Redact(p.Reason, 1024)
				// Preferences are optional; reserve current/linked facts first.
				in.Preferences = append(in.Preferences, p)
			}
		}
		if len(in.Preferences) > 8 {
			in.Preferences = in.Preferences[len(in.Preferences)-8:]
		}
	} else if !os.IsNotExist(err) {
		return in, err
	}
	for len(in.Preferences) > 0 {
		if err := w.checkInputSize(in); err == nil {
			break
		}
		in.Preferences = in.Preferences[1:]
	}
	// Nearest recent context first, with a stable tie-breaker. Keep entire
	// entries or omit them: slicing details would hide material uncertainty.
	sort.Slice(optional, func(i, j int) bool {
		if distance[optional[i].ID] != distance[optional[j].ID] {
			return distance[optional[i].ID] < distance[optional[j].ID]
		}
		return optional[i].ID < optional[j].ID
	})
	for _, e := range optional {
		if len(in.Outcomes) == 64 {
			break
		}
		in.Outcomes = append(in.Outcomes, e)
		if err := w.checkInputSize(in); err != nil {
			in.Outcomes = in.Outcomes[:len(in.Outcomes)-1]
		}
	}
	sort.Slice(in.Outcomes, func(i, j int) bool { return in.Outcomes[i].ID < in.Outcomes[j].ID })
	return in, w.checkInputSize(in)
}

// inputLimitError permits splitting an oversized batch without treating a
// storage/read failure as optional context or silently dropping required facts.
type inputLimitError struct{ message string }

func (e *inputLimitError) Error() string { return e.message }

func (w *Worker) checkInputSize(in Input) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if len(b) > w.Config.Runner.MaxInputBytes {
		return &inputLimitError{fmt.Sprintf("Athena input exceeds cap (%d bytes, limit %d)", len(b), w.Config.Runner.MaxInputBytes)}
	}
	return nil
}

func (w *Worker) makePlan(mode string, in Input, out Output, id string) Plan {
	now := w.now()
	p := Plan{Version: Version, ID: id, Mode: mode, CreatedAt: now.Format(time.RFC3339Nano), InputHash: capture.JSONHash(in), Input: in, Model: w.Config.Runner.Provider + "/" + w.Config.Runner.Model, Applied: map[string]string{}, Status: "ready", Operations: []Operation{}}
	byID := map[string]capture.Candidate{}
	for _, c := range in.Candidates {
		byID[c.ID] = c
	}
	for i, a := range out.Actions {
		op := Operation{ID: fmt.Sprintf("%s-%03d", id, i), Action: a}
		if a.Kind == "publish" || a.Kind == "amend" || a.Kind == "merge" {
			primary := byID[a.Candidates[0]]
			sources := []string{}
			seen := map[string]bool{}
			for _, cid := range a.Candidates {
				c := byID[cid]
				if !seen[c.Source] {
					sources = append(sources, c.Source)
					seen[c.Source] = true
				}
			}
			typ := a.Type
			if a.Kind != "publish" {
				typ = a.Kind
			}
			e := event.Event{Version: event.Version, ID: event.NewID(now), PublicationKey: "plan/" + op.ID, RecordedAt: now.Format(time.RFC3339Nano), OccurredAt: primary.OccurredAt, TimeBasis: primary.TimeBasis, Source: primary.Source, Type: typ, TLDR: a.Text, Details: a.Details, Tags: a.Tags, Refs: a.Refs, Context: primary.Context, Targets: a.Targets, Reason: a.Reason, Provenance: &event.Provenance{Candidates: a.Candidates, Evidence: a.Evidence, Sources: sources, Model: p.Model, Policy: PolicyVersion, Episode: primary.Episode}}
			e.Host, _ = os.Hostname()
			if a.Kind != "publish" {
				e.ToType = a.Type
			}
			op.Event = &e
		}
		p.Operations = append(p.Operations, op)
	}
	return p
}
func (w *Worker) apply(p *Plan) (int, error) {
	if p.Mode != "live" || p.Status != "ready" {
		return 0, fmt.Errorf("plan is not eligible for live application")
	}
	if capture.JSONHash(p.Input) != p.InputHash || p.Input.Policy != PolicyVersion {
		return 0, w.stopPlan(p, fmt.Errorf("saved plan input/policy mismatch; inspect the retained plan and use queue retry for fresh evaluation"))
	}
	out := Output{Version: Version}
	for _, op := range p.Operations {
		out.Actions = append(out.Actions, op.Action)
	}
	if err := Validate(p.Input, out); err != nil {
		return 0, w.stopPlan(p, err)
	}
	count := 0
	for _, op := range p.Operations {
		if op.Event == nil || p.Applied[op.ID] != "" {
			continue
		}
		id, err := store.AppendOnce(*op.Event)
		if err != nil {
			return count, w.stopPlan(p, err)
		}
		count++
		if w.AfterAppend != nil {
			if err := w.AfterAppend(); err != nil {
				return count, err
			}
		}
		p.Applied[op.ID] = id
		if err := durable.JSON(w.planPath(p.ID), p); err != nil {
			return count, err
		}
	}
	p.Status = "applied"
	if err := durable.JSON(w.planPath(p.ID), p); err != nil {
		return count, err
	}
	return count, w.ack(*p)
}

// Stop an incompatible plan without losing its history or already-applied work.
// Resolve the append-before-ack crash gap from ledger publication keys before
// making its reports explicitly retryable. No plan is deleted or auto-replayed.
func (w *Worker) stopPlan(p *Plan, cause error) error {
	all, err := store.ReadAllExisting()
	if err != nil {
		return err
	}
	stopped := *p
	stopped.Status, stopped.Error = "stale", cause.Error()
	stopped.Applied = map[string]string{}
	for op, id := range p.Applied {
		stopped.Applied[op] = id
	}
	for _, op := range p.Operations {
		if op.Event == nil {
			continue
		}
		for _, e := range all {
			if e.PublicationKey == op.Event.PublicationKey {
				stopped.Applied[op.ID] = e.ID
			}
		}
	}
	if err := durable.JSON(w.planPath(p.ID), stopped); err != nil {
		return err
	}
	if err := w.ack(stopped); err != nil {
		return err
	}
	*p = stopped
	return cause
}

func (w *Worker) ack(p Plan) error {
	items, err := w.Queue.List("")
	if err != nil {
		return err
	}
	receipts := map[string]capture.Receipt{}
	for _, it := range items {
		receipts[it.Candidate.ID] = it.Receipt
	}
	for _, c := range p.Input.Candidates {
		r := receipts[c.ID]
		if r.Status != "processing" || r.PlanID != p.ID {
			continue
		}
		r.Version = Version
		r.CandidateID = c.ID
		r.Status = "processed"
		r.PlanID = p.ID
		r.InputHash = p.InputHash
		r.Model = p.Model
		r.Policy = p.Input.Policy
		r.Evidence = c.Evidence
		r.AppliedEvents = []string{}
		r.Disposition = "outcome"
		reasons := []string{}
		for _, op := range p.Operations {
			for _, id := range op.Action.Candidates {
				if id == c.ID {
					if op.Action.Kind == "skip" || op.Action.Kind == "hold" {
						r.Disposition = op.Action.Kind
					}
					reasons = append(reasons, op.Action.Reason)
					if id := p.Applied[op.ID]; id != "" {
						r.AppliedEvents = append(r.AppliedEvents, id)
					}
				}
			}
		}
		r.Reason = strings.Join(reasons, "; ")
		if p.Status == "stale" {
			r.Status = "error"
			r.Reason = p.Error
		}
		r.UpdatedAt = w.now().Format(time.RFC3339Nano)
		if err := w.Queue.SaveReceipt(r); err != nil {
			return err
		}
	}
	return nil
}
func (w *Worker) fail(group []capture.Item, cause error) error {
	for _, it := range group {
		r := it.Receipt
		if r.Status != "processing" {
			r.Attempts++
		}
		r.Status = "error"
		r.PlanID = ""
		r.Reason = cause.Error()
		r.UpdatedAt = w.now().Format(time.RFC3339Nano)
		r.NextAttemptAt = w.now().Add(time.Duration(1<<min(r.Attempts, 8)) * time.Minute).Format(time.RFC3339Nano)
		if err := w.Queue.SaveReceipt(r); err != nil {
			return err
		}
	}
	return nil
}
