package poll

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/snapshot"
	"github.com/drdreo/daylog/internal/store"
)

type ScheduledResult struct {
	Attempted bool   `json:"attempted"`
	Output    string `json:"output,omitempty"`
}

type ghAttempt struct {
	At     time.Time `json:"at"`
	Owners string    `json:"owners"`
}

// ScheduledGH is independent of Athena's queue, model, and publication budget.
// Both successful snapshots and failed attempts throttle automatic requests;
// manual polling remains immediate and does not change the attempt timestamp.
func ScheduledGH(ctx context.Context, now time.Time, interval time.Duration, owners string) (ScheduledResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return scheduledGH(now, interval, owners, func(args ...string) ([]byte, error) {
		return runGHCommand(ctx, args...)
	})
}

func scheduledGH(now time.Time, interval time.Duration, owners string, run func(...string) ([]byte, error)) (ScheduledResult, error) {
	var result ScheduledResult
	if interval <= 0 {
		return result, nil
	}
	if err := store.Ensure(); err != nil {
		return result, err
	}
	root, err := store.DataDir()
	if err != nil {
		return result, err
	}
	unlock, err := durable.Lock(filepath.Join(root, "state", "gh-scheduled.lock"), false)
	if err != nil {
		return result, err
	}
	defer unlock()
	path := filepath.Join(root, "state", "gh-poll-attempt.json")
	var attempt ghAttempt
	if err := durable.Read(path, &attempt); err != nil && !os.IsNotExist(err) {
		return result, err
	}
	recent := func(at time.Time) bool {
		age := now.Sub(at)
		return age >= 0 && age < interval // recover from a backwards clock jump
	}
	if attempt.Owners == owners && recent(attempt.At) {
		return result, nil
	}
	// A manual refresh also postpones automatic polling. An owner configuration
	// change bypasses this check once we have an attempt for the previous scope.
	if attempt.At.IsZero() || attempt.Owners == owners {
		if snap, err := snapshot.LoadGHPRs(); err == nil && snap != nil {
			if fetched, err := time.Parse(time.RFC3339Nano, snap.FetchedAt); err == nil && recent(fetched) {
				return result, nil
			}
		}
	}
	if err := durable.JSON(path, ghAttempt{At: now, Owners: owners}); err != nil {
		return result, err
	}
	result.Attempted = true
	var output bytes.Buffer
	err = runGH(&output, &output, false, now, owners, run)
	result.Output = output.String()
	return result, err
}
