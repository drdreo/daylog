// Package snapshot owns the sidecar files under <data>/state/ — per-machine
// caches of external state maintained by pollers (§4.4). Snapshots are not
// events and are never synced: the event store holds the narrative, snapshots
// hold the now.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/store"
)

// PR is the current truth about one pull request, keyed by its typed ref.
type PR struct {
	Ref       string `json:"ref"`  // gh:pr:github.com/owner/repo#142
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
	Version   int           `json:"version"`
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
	var s GHPRs
	err = durable.Read(path, &s)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if s.Version != 2 {
		return nil, fmt.Errorf("unsupported PR snapshot version %d", s.Version)
	}
	return &s, nil
}

// SaveGHPRs replaces the snapshot atomically (write temp + rename), so a
// reader never sees a torn document.
func SaveGHPRs(s *GHPRs) error {
	if err := store.Ensure(); err != nil {
		return err
	}
	path, err := ghPRsPath()
	if err != nil {
		return err
	}
	copy := *s
	copy.Version = 2
	return durable.JSON(path, &copy)
}
