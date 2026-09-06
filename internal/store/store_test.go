package store

import (
	"github.com/drdreo/daylog/internal/event"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func sample() event.Event {
	n := time.Now()
	id := event.NewID(n)
	return event.Event{Version: 2, ID: id, PublicationKey: id, RecordedAt: n.Format(time.RFC3339Nano), OccurredAt: n.Add(-24 * time.Hour).Format(time.RFC3339Nano), TimeBasis: "human", Source: "human:cli", Type: "work", TLDR: "work", Refs: []string{}}
}
func TestConcurrentAppendAndReplay(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	e := sample()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := AppendOnce(e); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	all, err := ReadAll()
	if err != nil || len(all) != 1 {
		t.Fatal(all, err)
	}
	now, _ := time.Parse(time.RFC3339Nano, e.RecordedAt)
	p, _ := DayFile(now)
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
func TestTornAndMiddleCorruptionRefusePublication(t *testing.T) {
	for _, tail := range []string{`{"id":`, "not-json\n"} {
		t.Run(tail, func(t *testing.T) {
			t.Setenv("DAYLOG_DIR", t.TempDir())
			e := sample()
			if err := Append(e); err != nil {
				t.Fatal(err)
			}
			now, _ := time.Parse(time.RFC3339Nano, e.RecordedAt)
			p, _ := DayFile(now)
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
			f.WriteString(tail)
			f.Close()
			if _, err := AppendOnce(e); err == nil {
				t.Fatal("ambiguous dedup accepted")
			}
			if tail[len(tail)-1] != '\n' {
				if err := RepairTail(now); err != nil {
					t.Fatal(err)
				}
				backups, _ := filepath.Glob(p + ".damaged-*")
				if len(backups) != 1 {
					t.Fatal(backups)
				}
				if _, err := AppendOnce(e); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestStaleCorrection(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	e := sample()
	Append(e)
	a := sample()
	a.Type = event.TypeAmend
	a.Targets = []event.Target{{ID: e.ID, Revision: 1}}
	a.TLDR = "pinned"
	if err := Append(a); err != nil {
		t.Fatal(err)
	}
	b := sample()
	b.Type = event.TypeAmend
	b.Targets = a.Targets
	if err := Append(b); err == nil {
		t.Fatal("stale target accepted")
	}
	if _, err := AppendOnce(a); err != nil {
		t.Fatal("retry must precede revision check", err)
	}
}
