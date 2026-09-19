package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnsupportedFilesystemRefusedBeforeCreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	for _, write := range []bool{false, true} {
		err := prepareRoot(root, write, func(string) error { return fmt.Errorf("synthetic remote filesystem") })
		if err == nil {
			t.Fatal("unsupported filesystem accepted")
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("created root on unsupported filesystem")
		}
	}
}

func TestCanceledAttachedTransactionAndRecovery(t *testing.T) {
	s, root := openTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	a := put(t, s, fixture("a", "athena"))
	d := put(t, s, fixture("d", "dreo"))
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, db := range []string{"main", "peer"} {
		if _, e = tx.ExecContext(ctx, "UPDATE "+db+".heads SET lifecycle='invalidated'"); e != nil {
			t.Fatal(e)
		}
	}
	cancel()
	if e = tx.Commit(); e == nil {
		t.Fatal("canceled transaction committed")
	}
	_ = tx.Rollback()
	s.Close()
	next, e := Open(context.Background(), root, true)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	next.now = func() time.Time { return testNow }
	for _, r := range []Record{a, d} {
		if got, e := next.Show(context.Background(), r.Owner, r.ID, []string{r.Owner}); e != nil || got.Hash != r.Hash {
			t.Fatal("canceled changes escaped", e)
		}
	}
}
func TestSQLiteBusyDeadline(t *testing.T) {
	s, root := openTest(t)
	put(t, s, fixture("a", "athena"))
	s.Close()
	raw, e := sql.Open("sqlite", filepath.Join(root, "athena.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer raw.Close()
	if _, e = raw.Exec("BEGIN EXCLUSIVE"); e != nil {
		t.Fatal(e)
	}
	defer raw.Exec("ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if opened, e := Open(ctx, root, false, "athena"); e == nil {
		opened.Close()
		t.Fatal("exclusive lock ignored")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("busy timeout exceeded bound")
	}
}
func TestRejectUnrelatedNamespaceAndDatabase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "unrelated")
	os.WriteFile(marker, []byte("preserve"), 0600)
	if _, e := Open(context.Background(), root, true); e == nil {
		t.Fatal("unrelated namespace adopted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("changed unrelated root")
	}
	os.Remove(marker)
	path := filepath.Join(root, "athena.sqlite")
	os.WriteFile(path, nil, 0600)
	raw, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = raw.Exec("CREATE TABLE unrelated(value TEXT); PRAGMA user_version=1"); e != nil {
		t.Fatal(e)
	}
	raw.Close()
	before, _ := os.ReadFile(path)
	if _, e = Open(context.Background(), root, true); e == nil {
		t.Fatal("unrelated DB adopted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("unrelated DB modified")
	}
}
func TestStaleIndexRowsCannotExposeCanonicalOrOldText(t *testing.T) {
	s, _ := openTest(t)
	r := put(t, s, fixture("a", "athena"))
	change := r.Input
	change.Content = "new amber"
	if _, e := s.Correct(context.Background(), "athena", r.ID, 1, change, false); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("INSERT INTO main.search(id,revision,content) VALUES('a',1,'cobalt')"); e != nil {
		t.Fatal(e)
	}
	if got := search(t, s, Filter{Owners: []string{"athena"}, Query: "cobalt", Limit: 10}); len(got) != 0 {
		t.Fatal("stale revision escaped index", got)
	}
	if e := s.Forget(context.Background(), "athena", "a", 2); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("INSERT INTO main.search(id,revision,content) VALUES('a',3,'cobalt')"); e != nil {
		t.Fatal(e)
	}
	if got := search(t, s, Filter{Owners: []string{"athena"}, Query: "cobalt", Limit: 10}); len(got) != 0 {
		t.Fatal("tombstone escaped index")
	}
}
func TestCrossOwnerPaginationIncludesScopedDerivatives(t *testing.T) {
	s, _ := openTest(t)
	d := put(t, s, fixture("same", "dreo"))
	put(t, s, derived("same", d))
	first := search(t, s, Filter{Owners: []string{"athena", "dreo"}, Limit: 1})
	if len(first) != 1 || first[0].Owner != "athena" {
		t.Fatal(first)
	}
	next := search(t, s, Filter{Owners: []string{"athena", "dreo"}, Limit: 1, AfterID: first[0].Owner + ":" + first[0].ID})
	if len(next) != 1 || next[0].Owner != "dreo" {
		t.Fatal(next)
	}
	last := search(t, s, Filter{Owners: []string{"athena", "dreo"}, Limit: 1, AfterID: "dreo:same"})
	if len(last) != 0 {
		t.Fatal(last)
	}
	if _, err := s.db.Exec("DROP TABLE main.search"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := search(t, s, Filter{Owners: []string{"athena", "dreo"}, Query: "zircon", Limit: 1}); len(got) != 1 {
		t.Fatal(got)
	}
}

func BenchmarkIndexedMemory1000(b *testing.B) {
	root := filepath.Join(b.TempDir(), "memory")
	s, e := Open(context.Background(), root, true)
	if e != nil {
		b.Fatal(e)
	}
	defer s.Close()
	s.now = func() time.Time { return testNow }
	for i := 0; i < 1000; i++ {
		in := fixture(fmt.Sprintf("r%04d", i), "athena")
		if _, e = s.Record(context.Background(), in, false); e != nil {
			b.Fatal(e)
		}
	}
	b.Run("recall", func(b *testing.B) {
		for b.Loop() {
			if _, e = s.Search(context.Background(), Filter{Owners: []string{"athena"}, Query: "cobalt", Limit: 10}); e != nil {
				b.Fatal(e)
			}
		}
	})
	b.Run("rebuild", func(b *testing.B) {
		for b.Loop() {
			if e = s.Rebuild(context.Background()); e != nil {
				b.Fatal(e)
			}
		}
	})
}
