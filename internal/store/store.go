// Package store owns fresh-store initialization and serialized replay-safe publication.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/event"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const Version = 2

var BuildVersion = "athena-dev"

func DataDir() (string, error) {
	if d := os.Getenv("DAYLOG_DIR"); d != "" {
		return filepath.Abs(d)
	}
	if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			h, e := os.UserHomeDir()
			if e != nil {
				return "", e
			}
			base = filepath.Join(h, ".local", "share")
		}
		return filepath.Join(base, "daylog"), nil
	}
	base, e := os.UserConfigDir()
	return filepath.Join(base, "daylog"), e
}

type Manifest struct {
	Version  int    `json:"version"`
	Protocol string `json:"protocol"`
}

func Ensure() error {
	root, err := DataDir()
	if err != nil {
		return err
	}
	// Initialization lock lives beside the root so an unsupported directory is untouched.
	unlock, err := durable.Lock(root+".init.lock", true)
	if err != nil {
		return err
	}
	defer unlock()
	var m Manifest
	err = durable.Read(filepath.Join(root, "store.json"), &m)
	if err == nil {
		if m.Version != Version || m.Protocol != "local-append-once-v2" {
			return fmt.Errorf("unsupported store version/protocol; choose a fresh DAYLOG_DIR")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("unsupported unversioned data at %s; choose a fresh DAYLOG_DIR (no migration)", root)
	}
	if err := durable.Mkdir(root); err != nil {
		return err
	}
	if err := durable.Private(root, true); err != nil {
		return err
	}
	// Marker first: incomplete directory creation is safe to resume.
	return durable.JSON(filepath.Join(root, "store.json"), Manifest{Version, "local-append-once-v2"})
}
func DayFile(day time.Time) (string, error) {
	r, e := DataDir()
	return filepath.Join(r, "events", day.Format("2006-01-02")+".jsonl"), e
}
func ledgerLock() (func(), error) {
	if err := Ensure(); err != nil {
		return nil, err
	}
	r, _ := DataDir()
	return durable.Lock(filepath.Join(r, "store.lock"), true)
}
func Append(e event.Event) error { _, err := AppendOnce(e); return err }

// AppendOnce scans under the same lock as every human and automated writer.
// A successful retry returns the original ID before checking stale revisions.
func AppendOnce(e event.Event) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	unlock, err := ledgerLock()
	if err != nil {
		return "", err
	}
	defer unlock()
	all, err := readAll()
	if err != nil {
		return "", err
	}
	for _, old := range all {
		if old.PublicationKey == e.PublicationKey {
			return old.ID, nil
		}
	}
	if err := event.CheckTargets(e, event.Effective(all)); err != nil {
		return "", err
	}
	e.Sequence = len(all) + 1
	ts, _ := time.Parse(time.RFC3339Nano, e.RecordedAt)
	path, _ := DayFile(ts)
	if err := durable.Mkdir(filepath.Dir(path)); err != nil {
		return "", err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	if len(line) > 1024*1024-1 {
		return "", fmt.Errorf("event exceeds ledger record cap")
	}
	line = append(line, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", err
	}
	if err = durable.Private(path, false); err == nil {
		var n int
		n, err = f.Write(line)
		if err == nil && n != len(line) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return "", err
	}
	if err = durable.SyncDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	return e.ID, nil
}
func ReadAll() ([]event.Event, error) {
	u, e := ledgerLock()
	if e != nil {
		return nil, e
	}
	defer u()
	return readAll()
}
func ReadDay(day time.Time) ([]event.Event, error) {
	all, err := ReadAll()
	if err != nil {
		return nil, err
	}
	out := []event.Event{}
	for _, e := range all {
		if strings.HasPrefix(e.RecordedAt, day.Format("2006-01-02")) {
			out = append(out, e)
		}
	}
	return out, nil
}
func readAll() ([]event.Event, error) {
	root, _ := DataDir()
	paths, err := filepath.Glob(filepath.Join(root, "events", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	all := []event.Event{}
	keys := map[string]bool{}
	ids := map[string]bool{}
	for _, p := range paths {
		evs, err := readFile(p)
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			if keys[e.PublicationKey] || ids[e.ID] {
				return nil, fmt.Errorf("duplicate publication key/id in %s", p)
			}
			keys[e.PublicationKey] = true
			ids[e.ID] = true
			all = append(all, e)
		}
	}
	// Local sequence is assigned under the shared lock, independent of clock skew.
	sort.Slice(all, func(i, j int) bool { return all[i].Sequence < all[j].Sequence })
	for i, e := range all {
		if e.Sequence != i+1 {
			return nil, fmt.Errorf("ambiguous ledger sequence at %s", e.ID)
		}
	}
	return all, nil
}
func readFile(path string) ([]event.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1024*1024)
	out := []event.Event{}
	for lineNo := 1; ; lineNo++ {
		line, err := r.ReadSlice('\n')
		if len(line) > 1024*1024 {
			return nil, fmt.Errorf("oversized ledger record %s:%d", path, lineNo)
		}
		if err == io.EOF && len(line) == 0 {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("torn trailing record %s:%d; publication refused; preserve bytes and run explicit repair: %w", path, lineNo, err)
		}
		var e event.Event
		if err := durable.Decode(bytes.TrimSuffix(line, []byte{'\n'}), &e); err != nil {
			return nil, fmt.Errorf("malformed ledger %s:%d: %w", path, lineNo, err)
		}
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("invalid ledger %s:%d: %w", path, lineNo, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// RepairTail only removes an unterminated last line, saving all original bytes.
// Malformed complete records are never automatically repaired.
func RepairTail(day time.Time) error {
	u, err := ledgerLock()
	if err != nil {
		return err
	}
	defer u()
	p, _ := DayFile(day)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return fmt.Errorf("no torn tail")
	}
	backup := p + ".damaged-" + event.NewID(time.Now())
	if err := durable.Write(backup, b); err != nil {
		return err
	}
	end := bytes.LastIndexByte(b, '\n') + 1
	return durable.Write(p, b[:end])
}
