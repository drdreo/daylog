package capture

import (
	"github.com/drdreo/daylog/internal/durable"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func candidate() Candidate {
	at := time.Now().Format(time.RFC3339Nano)
	return Candidate{Version: 2, Source: "agent:pi", Kind: "work", Text: "Report claim", CapturedAt: at, OccurredAt: at, TimeBasis: "report", Origin: "report", Completeness: "claim", Terminal: "unknown"}
}
func TestKeyedConcurrentEnqueueIndependentOfWorker(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	q, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	u, err := durable.Lock(filepath.Join(q.Root, "worker.lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer u()
	c := candidate()
	c.IdempotencyKey = "one"
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := q.Enqueue(c); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	items, err := q.List("pending")
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	c.Text = "changed"
	if _, err := q.Enqueue(c); err == nil {
		t.Fatal("key collision accepted")
	}
}
func TestNativeRevisionAndUnkeyedReports(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	q, _ := Open()
	c := candidate()
	c.Context.Session = "s"
	c.NativeID = "message"
	c.Revision = "1"
	q.Enqueue(c)
	q.Enqueue(c)
	c.Revision = "2"
	q.Enqueue(c)
	c.NativeID = ""
	q.Enqueue(c)
	q.Enqueue(c)
	items, e := q.List("")
	if e != nil || len(items) != 4 {
		t.Fatal(len(items), e)
	}
}
