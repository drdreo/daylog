package config

import (
	"github.com/drdreo/daylog/internal/store"
	"os"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	c, e := Load()
	if e != nil || c.Mode != "live" || c.GHPollSeconds != 300 {
		t.Fatal(c, e)
	}
	p, _ := Path()
	for _, raw := range []string{`{"version":1}`, `{"version":2,"mode":"direct"}`, `{"version":2,"unknown":true}`, `{"version":2} trailing`, `{"version":2,"runner":{"calls_per_run":0}}`, `{"version":2,"github_poll_seconds":-1}`, `{"version":2,"github_poll_seconds":59}`, `{"version":2,"github_poll_seconds":86401}`} {
		if e := os.WriteFile(p, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := Load(); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestGHPollIntervalCompatibility(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	for raw, want := range map[string]int{`{"version":2}`: 300, `{"version":2,"github_poll_seconds":0}`: 0, `{"version":2,"github_poll_seconds":600}`: 600} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil || cfg.GHPollSeconds != want {
			t.Fatal(cfg, err)
		}
	}
}

func TestUnsupportedStoreUntouched(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYLOG_DIR", dir)
	p := dir + "/old.jsonl"
	os.WriteFile(p, []byte("old"), 0600)
	if store.Ensure() == nil {
		t.Fatal("accepted legacy data")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "old" {
		t.Fatal("modified history")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("mutated unsupported store")
	}
}
