// Package store owns the on-disk layout: one JSONL file per day under the
// platform's user data directory. Append-only; nothing here mutates history.
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/event"
)

// DataDir resolves the daylog root. Precedence: $DAYLOG_DIR, then the
// platform's conventional per-user data directory:
//
//	linux:   $XDG_DATA_HOME/daylog (default ~/.local/share/daylog)
//	darwin:  ~/Library/Application Support/daylog
//	windows: %AppData%\daylog
func DataDir() (string, error) {
	if dir := os.Getenv("DAYLOG_DIR"); dir != "" {
		return dir, nil
	}
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "daylog"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Join(home, ".local", "share", "daylog"), nil
	}
	base, err := os.UserConfigDir() // AppData on Windows, Application Support on macOS
	if err != nil {
		return "", fmt.Errorf("resolve user data dir: %w", err)
	}
	return filepath.Join(base, "daylog"), nil
}

func eventsDir() (string, error) {
	root, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "events"), nil
}

// DayFile returns the JSONL path for a date (one source of truth per day).
func DayFile(day time.Time) (string, error) {
	dir, err := eventsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, day.Format("2006-01-02")+".jsonl"), nil
}

// Append writes one event as one line using O_APPEND, which is atomic for
// writes of this size on Linux — concurrent agents need no locking (§5).
func Append(e event.Event) error {
	ts, err := time.Parse(time.RFC3339, e.TS)
	if err != nil {
		return fmt.Errorf("event has invalid ts %q: %w", e.TS, err)
	}
	path, err := DayFile(ts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create events dir: %w", err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append to %s: %w", path, err)
	}
	return f.Close()
}

// ReadDay loads one day's events. A missing file is an empty day, not an
// error. Torn or malformed lines are skipped, never fatal (§8: line-oriented
// parsing skips a torn final line).
func ReadDay(day time.Time) ([]event.Event, error) {
	path, err := DayFile(day)
	if err != nil {
		return nil, err
	}
	return readFile(path)
}

// ReadAll loads every day file in the store, sorted by ULID. Used to fold
// cross-day state (a todo opened Monday is still open Thursday).
func ReadAll() ([]event.Event, error) {
	dir, err := eventsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read events dir: %w", err)
	}
	var all []event.Event
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".jsonl") {
			continue
		}
		evs, err := readFile(filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, err
		}
		all = append(all, evs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}

func readFile(path string) ([]event.Event, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var evs []event.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e event.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.ID == "" {
			continue // torn or foreign line: tolerate, never crash (§10)
		}
		evs = append(evs, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return evs, nil
}
