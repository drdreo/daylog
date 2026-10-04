package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

var recallRepo = event.Repository{Host: "github.com", Path: "team/project"}

func recallFixture(t *testing.T) (string, *capture.Spool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DAYLOG_DIR", filepath.Join(home, "data"))
	q, err := capture.Open()
	if err != nil {
		t.Fatal(err)
	}
	return home, q
}
func recallCandidate(t *testing.T, q *capture.Spool, id, at string, repo event.Repository) capture.Candidate {
	t.Helper()
	c, err := q.Enqueue(capture.Candidate{Version: 2, ID: id, Source: "agent:pi", Kind: "work", Text: "Reported work " + id,
		Refs: []string{}, CapturedAt: "2026-10-04T10:00:00Z", OccurredAt: at, TimeBasis: "report",
		Context: event.Context{Repository: repo, Cwd: "/same/path", Worktree: "/same/path"}, Origin: "report", Completeness: "claim", Terminal: "unknown", Evidence: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func recallEntry(t *testing.T, text, at string, repo event.Repository, candidate string) event.Event {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t.Fatal(err)
	}
	e := newEvent(tm, "human:cli", "work", text)
	e.Context = event.Context{Repository: repo, Cwd: "/same/path"}
	if candidate != "" {
		e.Source = "agent:pi"
		e.Provenance = &event.Provenance{Candidates: []string{candidate}, Sources: []string{"agent:pi"}}
	}
	if err := store.Append(e); err != nil {
		t.Fatal(err)
	}
	return e
}
func recallResult(t *testing.T, home string, extra ...string) projectRecall {
	t.Helper()
	out, err := runCLI(t, home, "agent:pi", append([]string{"recall", "--project", recallRepo.Key()}, extra...)...)
	if err != nil {
		t.Fatal(out, err)
	}
	var result projectRecall
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(out, err)
	}
	if result.Version != 1 || result.Project != recallRepo {
		t.Fatal(out)
	}
	return result
}

func TestRecallExactProjectIsolation(t *testing.T) {
	home, q := recallFixture(t)
	at := "2026-10-01T10:00:00Z"
	for i, repo := range []event.Repository{recallRepo, {Host: "elsewhere.example", Path: recallRepo.Path}, {Host: recallRepo.Host, Path: "other/project"}, {Host: recallRepo.Host, Path: "team/Project"}, {}, {Host: recallRepo.Host, Path: "team/project-extra"}} {
		id := []string{"selected", "other-host", "other-owner", "other-case", "missing", "prefix"}[i]
		recallCandidate(t, q, id, at, repo)
		recallEntry(t, id, at, repo, "")
	}
	// Refs/cwd/worktree do not grant scope. Unrelated receipts aren't read.
	if err := os.WriteFile(filepath.Join(q.Root, "receipts", "other-host.json"), []byte("broken unrelated receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	r := recallResult(t, home)
	if len(r.Entries) != 1 || r.Entries[0].TLDR != "selected" || len(r.Reports) != 1 || r.Reports[0].Candidate.ID != "selected" {
		t.Fatalf("%+v", r)
	}
	if r.Coverage.CompleteHistory || r.Coverage.AtomicSnapshot || r.Coverage.ReportMatches != 1 {
		t.Fatalf("%+v", r.Coverage)
	}
}

func TestRecallRejectsMissingAmbiguousAndUnboundedScopeBeforeRead(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{
		{"recall"}, {"recall", "--project", "project"}, {"recall", "--project", "team/project"},
		{"recall", "--project", "https://github.com/team/project"}, {"recall", "--project", "github.com/team/*"},
		{"recall", "--project", recallRepo.Key(), "--project", "github.com/other/repo"},
		{"recall", "--project", "/absolute/project"}, {"recall", "--project", "github.com/../project"},
		{"recall", "--project", recallRepo.Key(), "--limit", "0"}, {"recall", "--project", recallRepo.Key(), "--limit", "6"},
		{"recall", "--project", recallRepo.Key(), "--limit", "-1"}, {"recall", "extra", "--project", recallRepo.Key()},
	} {
		out, err := runCLI(t, home, "agent:pi", args...)
		if err == nil || strings.Contains(out, `"entries"`) {
			t.Fatal(args, out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "data")); !os.IsNotExist(err) {
		t.Fatal("initialized missing store", err)
	}
}

func TestRecallQueueStateIsNotPublication(t *testing.T) {
	home, q := recallFixture(t)
	at := "2026-10-01T10:00:00Z"
	for _, id := range []string{"pending", "hold", "skip", "error", "published"} {
		c := recallCandidate(t, q, id, at, recallRepo)
		r := capture.Receipt{Version: 2, CandidateID: c.ID, UpdatedAt: at, Status: "processed", AppliedEvents: []string{}}
		switch id {
		case "pending":
			continue
		case "hold", "skip":
			r.Disposition = id
		case "error":
			r.Status = "error"
			r.Disposition = "outcome"
			r.AppliedEvents = []string{"unacknowledged-nonexistent-event"}
		case "published":
			e := recallEntry(t, "Published once", at, recallRepo, c.ID)
			r.Disposition = "outcome"
			r.AppliedEvents = []string{e.ID}
		}
		if err := q.SaveReceipt(r); err != nil {
			t.Fatal(err)
		}
	}
	r := recallResult(t, home)
	if len(r.Reports) != 5 || len(r.Entries) != 1 {
		t.Fatalf("%+v", r)
	}
	for _, report := range r.Reports {
		if report.Candidate.ID == "published" {
			if report.PublicationState != "recorded" || len(report.RecordedEvents) != 1 || report.RecordedEvents[0].EntryIDs[0] != r.Entries[0].ID {
				t.Fatalf("%+v", report)
			}
		} else if report.PublicationState != "not_observed" || len(report.RecordedEvents) != 0 {
			t.Fatalf("%+v", report)
		}
	}
}

func TestRecallCrashLinksAndCurrentCorrectedState(t *testing.T) {
	home, q := recallFixture(t)
	at := "2026-10-01T10:00:00Z"
	c := recallCandidate(t, q, "crash-before-ack", at, recallRepo)
	e := recallEntry(t, "Stale original", at, recallRepo, c.ID)
	// No receipt acknowledgment: ledger provenance must still reveal the write.
	amend := newEvent(time.Now(), "human:cli", "amend", "Corrected, not verified")
	amend.Targets = []event.Target{{ID: e.ID, Revision: 1}}
	// Correction can come from a different cwd/repository. Target identity wins.
	amend.Context.Repository = event.Repository{Host: "github.com", Path: "other/repo"}
	if err := store.Append(amend); err != nil {
		t.Fatal(err)
	}
	dismiss := newEvent(time.Now(), "human:cli", "dismiss", "")
	dismiss.Targets = []event.Target{{ID: e.ID, Revision: 2}}
	if err := store.Append(dismiss); err != nil {
		t.Fatal(err)
	}
	r := recallResult(t, home)
	if r.Entries[0].TLDR != amend.TLDR || !r.Entries[0].Dismissed || !r.Entries[0].Pinned || r.Entries[0].Revision != 3 {
		t.Fatalf("%+v", r.Entries)
	}
	if r.Reports[0].Receipt.Status != "pending" || r.Reports[0].PublicationState != "recorded" {
		t.Fatalf("%+v", r.Reports)
	}
}

func TestRecallMergedContributorsRemainLinkedNotIndependentOutcomes(t *testing.T) {
	home, q := recallFixture(t)
	at := "2026-10-01T10:00:00Z"
	for _, id := range []string{"first", "second"} {
		recallCandidate(t, q, id, at, recallRepo)
	}
	first := recallEntry(t, "First wording", at, recallRepo, "first")
	second := recallEntry(t, "Second wording", at, recallRepo, "second")
	merge := newEvent(time.Now(), "human:cli", "merge", "One combined outcome")
	merge.Targets = []event.Target{{ID: first.ID, Revision: 1}, {ID: second.ID, Revision: 1}}
	if err := store.Append(merge); err != nil {
		t.Fatal(err)
	}
	r := recallResult(t, home)
	byID := map[string]event.Entry{}
	for _, entry := range r.Entries {
		byID[entry.ID] = entry
	}
	if byID[first.ID].TLDR != merge.TLDR || len(byID[first.ID].Contributors) != 2 || byID[second.ID].MergedInto != first.ID {
		t.Fatalf("%+v", r.Entries)
	}
	for _, report := range r.Reports {
		if report.PublicationState != "recorded" || len(report.RecordedEvents) != 1 {
			t.Fatalf("%+v", report)
		}
	}
}

func TestRecallLimitsOrderAndEmptyCoverage(t *testing.T) {
	home, q := recallFixture(t)
	r := recallResult(t, home)
	if r.Entries == nil || r.Reports == nil || r.Coverage.EntryMatches != 0 || r.Coverage.ReportMatches != 0 {
		t.Fatalf("%+v", r)
	}
	for _, v := range []struct{ id, at string }{
		{"z", "2026-10-01T10:00:00+02:00"}, {"b", "2026-10-01T09:00:00Z"}, {"a", "2026-10-01T09:00:00Z"},
	} {
		recallCandidate(t, q, v.id, v.at, recallRepo)
		recallEntry(t, v.id, v.at, recallRepo, "")
	}
	r = recallResult(t, home, "--limit", "2")
	if len(r.Entries) != 2 || len(r.Reports) != 2 || r.Reports[0].Candidate.ID != "a" || r.Reports[1].Candidate.ID != "b" || r.Coverage.EntriesOmitted != 1 || r.Coverage.ReportsOmitted != 1 {
		t.Fatalf("%+v", r)
	}
	if !recallNewer("2026-10-01T09:00:00Z", "a", "2026-10-01T11:00:00+02:00", "b") {
		t.Fatal("equal instants must use ID tie-break")
	}
}

func TestRecallReadAndOutputFailuresWithholdAllEvidence(t *testing.T) {
	for _, failure := range []string{"missing-store", "bad-ledger", "unreadable-ledger", "bad-candidate", "bad-receipt", "unreadable-queue", "oversized"} {
		t.Run(failure, func(t *testing.T) {
			home, q := recallFixture(t)
			c := recallCandidate(t, q, "report", "2026-10-01T10:00:00Z", recallRepo)
			recallEntry(t, "SECRET-MUST-NOT-LEAK-ON-FAILURE", c.OccurredAt, recallRepo, "")
			switch failure {
			case "missing-store":
				if err := os.Remove(filepath.Join(home, "data", "store.json")); err != nil {
					t.Fatal(err)
				}
			case "bad-ledger":
				if err := os.WriteFile(filepath.Join(home, "data", "events", "2026-10-01.jsonl"), []byte("private malformed text\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable-ledger":
				if err := os.RemoveAll(filepath.Join(home, "data", "events")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, "data", "events"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			case "bad-candidate", "bad-receipt":
				dir := "candidates"
				if failure == "bad-receipt" {
					dir = "receipts"
				}
				if err := os.WriteFile(filepath.Join(q.Root, dir, c.ID+".json"), []byte("private malformed text"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable-queue":
				if err := os.RemoveAll(filepath.Join(q.Root, "candidates")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(q.Root, "candidates"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				for _, id := range []string{"large-a", "large-b", "large-c"} {
					large := c
					large.ID = id
					large.Text = strings.Repeat("x", capture.MaxReportBytes)
					if _, err := q.Enqueue(large); err != nil {
						t.Fatal(err)
					}
				}
			}
			cmd := exec.Command(testBinary, "--data-dir", filepath.Join(home, "data"), "recall", "--project", recallRepo.Key())
			cmd.Env = cliEnv(home, "agent:pi")
			out, err := cmd.Output()
			if err == nil || len(out) != 0 {
				t.Fatal("failure returned evidence", string(out), err)
			}
			if exit, ok := err.(*exec.ExitError); ok && (strings.Contains(string(exit.Stderr), "private malformed text") || strings.Contains(string(exit.Stderr), "SECRET-MUST")) {
				t.Fatal("failure leaked source", string(exit.Stderr))
			}
		})
	}
}

func TestRecallDoesNotReadEvidencePlansOrWriteReports(t *testing.T) {
	home, q := recallFixture(t)
	c := recallCandidate(t, q, "pending", "2026-10-01T10:00:00Z", recallRepo)
	evidence, err := q.PutEvidence("native excerpt excluded", "claim", c.CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	native := c
	native.ID, native.Origin, native.Evidence = "native", "hook", []string{evidence.ID}
	if _, err := q.Enqueue(native); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(q.Root, "evidence", evidence.ID+".json")); err != nil {
		t.Fatal(err)
	}
	if err := q.SaveReceipt(capture.Receipt{Version: 2, CandidateID: native.ID, Status: "processing", UpdatedAt: c.CapturedAt, AppliedEvents: []string{}}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"evidence", "plans"} {
		if err := os.WriteFile(filepath.Join(q.Root, dir, "unreadable.json"), []byte("not JSON"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := q.List("")
	if err != nil {
		t.Fatal(err)
	}
	r := recallResult(t, home)
	after, err := q.List("")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || len(r.Reports) != 2 || r.Reports[0].Candidate.ID != native.ID || r.Reports[0].Receipt.Status != "processing" || r.Reports[0].PublicationState != "not_observed" {
		t.Fatal("read mutated queue or lost native/processing state")
	}
	if _, err := os.Stat(filepath.Join(home, "data", "events")); !os.IsNotExist(err) {
		t.Fatal("read published", err)
	}
}
