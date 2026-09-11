package poll

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/snapshot"
	"path/filepath"
)

func TestScheduledGHCadenceAndFailure(t *testing.T) {
	root := tempDataDir(t)
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	calls := 0
	fail := false
	run := func(args ...string) ([]byte, error) {
		calls++
		if fail {
			return nil, fmt.Errorf("offline")
		}
		return []byte(`[]`), nil
	}
	poll := func(at time.Time, owners string, want bool) ScheduledResult {
		t.Helper()
		result, err := scheduledGH(at, 5*time.Minute, owners, run)
		if err != nil || result.Attempted != want {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		return result
	}
	poll(now, "o", true)
	poll(now.Add(time.Minute), "o", false)
	fail = true
	result := poll(now.Add(5*time.Minute), "o", true)
	if !strings.Contains(result.Output, "keeping previous snapshot") {
		t.Fatal(result)
	}
	snap, err := snapshot.LoadGHPRs()
	if err != nil || snap.FetchedAt != now.Format(time.RFC3339) {
		t.Fatal(snap, err)
	}
	poll(now.Add(6*time.Minute), "o", false) // failed attempts are throttled too
	fail = false
	poll(now.Add(10*time.Minute), "o", true)
	poll(now.Add(11*time.Minute), "other", true) // changed scope is immediately due
	poll(now.Add(-time.Minute), "other", true)   // backwards clock doesn't stall polling
	if calls != 5 {
		t.Fatalf("calls=%d", calls)
	}
	unlock, err := durable.Lock(filepath.Join(root, "state", "gh-scheduled.lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := scheduledGH(now.Add(time.Hour), time.Minute, "o", run); err == nil {
		t.Fatal("overlapping scheduled poll allowed")
	}
}

func TestScheduledGHRespectsManualRefreshAndDisable(t *testing.T) {
	tempDataDir(t)
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	if err := snapshot.SaveGHPRs(&snapshot.GHPRs{FetchedAt: now.Format(time.RFC3339), PRs: map[string]snapshot.PR{}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	run := func(...string) ([]byte, error) { calls++; return []byte(`[]`), nil }
	for _, interval := range []time.Duration{0, 5 * time.Minute} {
		result, err := scheduledGH(now.Add(time.Minute), interval, "", run)
		if err != nil || result.Attempted {
			t.Fatal(result, err)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	result, err := scheduledGH(now.Add(5*time.Minute), 5*time.Minute, "", run)
	if err != nil || !result.Attempted || calls != 1 {
		t.Fatal(result, calls, err)
	}
}

func TestGHCommandHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runGHCommand(ctx, "search", "prs"); err == nil {
		t.Fatal("cancelled command succeeded")
	}
}
