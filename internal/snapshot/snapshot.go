// Package snapshot owns the sidecar files under <data>/state/ — per-machine
// caches of external state maintained by pollers (§4.4). Snapshots are not
// events and are never synced: the event store holds the narrative, snapshots
// hold the now.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/drdreo/daylog/internal/store"
)

// PR is the current truth about one pull request, keyed by its typed ref.
type PR struct {
	Ref       string `json:"ref"`  // gh:pr:owner/repo#142
	Repo      string `json:"repo"` // owner/repo
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"` // open | merged | closed
	Draft     bool   `json:"draft"`
	Checks    string `json:"checks"` // passing | failing | pending | none
	Review    string `json:"review"` // approved | changes_requested | review_required | none
	UpdatedAt string `json:"updated_at"`
}

// GHPRs is the document in <data>/state/gh-prs.json. FetchedAt is the
// honesty marker: a poller that cannot fetch leaves the old document (and
// its old FetchedAt) in place rather than pretending freshness (§6).
type GHPRs struct {
	FetchedAt string        `json:"fetched_at"`
	PRs       map[string]PR `json:"prs"`
}

func ghPRsPath() (string, error) {
	root, err := store.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", "gh-prs.json"), nil
}

// LoadGHPRs reads the snapshot. A missing file means the poller has never
// completed a fetch on this machine: (nil, nil), not an error.
func LoadGHPRs() (*GHPRs, error) {
	path, err := ghPRsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s GHPRs
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &s, nil
}

// SaveGHPRs replaces the snapshot atomically (write temp + rename), so a
// reader never sees a torn document.
func SaveGHPRs(s *GHPRs) error {
	path, err := ghPRsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gh-prs-*.json")
	if err != nil {
		return fmt.Errorf("create temp snapshot: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp snapshot: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace snapshot: %w", err)
	}
	return nil
}
