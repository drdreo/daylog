package athena

import (
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/durable"
	"os"
	"path/filepath"
	"time"
)

// Prune removes only separately retained excerpts after all referencing inputs
// were processed and aged out. Candidate/receipt identities remain for retries.
func Prune(q *capture.Spool, days int, now time.Time) (int, error) {
	u, err := durable.Lock(filepath.Join(q.Root, "worker.lock"), false)
	if err != nil {
		return 0, err
	}
	defer u()
	intake, err := durable.Lock(filepath.Join(q.Root, "intake.lock"), true)
	if err != nil {
		return 0, err
	}
	defer intake()
	items, err := q.List("")
	if err != nil {
		return 0, err
	}
	eligible := map[string]bool{}
	blocked := map[string]bool{}
	for _, it := range items {
		at, e := time.Parse(time.RFC3339Nano, it.Receipt.UpdatedAt)
		old := e == nil && it.Receipt.Status == "processed" && now.Sub(at) > time.Duration(days)*24*time.Hour
		for _, id := range it.Candidate.Evidence {
			if old {
				eligible[id] = true
			} else {
				blocked[id] = true
			}
		}
	}
	paths, err := filepath.Glob(filepath.Join(q.Root, "plans", "*.json"))
	if err != nil {
		return 0, err
	}
	plans := map[string]Plan{}
	for _, path := range paths {
		var p Plan
		if err := durable.Read(path, &p); err != nil {
			return 0, err
		}
		if p.Version != Version {
			return 0, fmt.Errorf("unsupported plan")
		}
		plans[path] = p
		if p.Status != "applied" && p.Status != "shadow" {
			for _, e := range p.Input.Evidence {
				blocked[e.ID] = true
			}
		}
	}
	// Redact copied input excerpts before deleting the separately retained file.
	// The original input hash and evidence hashes remain inspectable.
	for path, p := range plans {
		changed := false
		for i, e := range p.Input.Evidence {
			if eligible[e.ID] && !blocked[e.ID] && e.Text != "" {
				p.Input.Evidence[i].Text = ""
				changed = true
			}
		}
		if changed {
			p.EvidencePruned = true
			if err := durable.JSON(path, p); err != nil {
				return 0, err
			}
		}
	}
	n := 0
	for id := range eligible {
		if blocked[id] {
			continue
		}
		if !capture.SafeID(id) {
			return n, fmt.Errorf("invalid evidence identity")
		}
		if err := os.Remove(filepath.Join(q.Root, "evidence", id+".json")); err == nil {
			n++
		} else if !os.IsNotExist(err) {
			return n, err
		}
	}
	return n, durable.SyncDir(filepath.Join(q.Root, "evidence"))
}
