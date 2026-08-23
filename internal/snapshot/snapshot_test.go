package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingIsNilNotError(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	s, err := LoadGHPRs()
	if err != nil || s != nil {
		t.Fatalf("Load on empty store = (%v, %v), want (nil, nil)", s, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYLOG_DIR", dir)
	in := &GHPRs{
		FetchedAt: "2026-08-23T12:00:00Z",
		PRs: map[string]PR{
			"gh:pr:o/r#7": {Ref: "gh:pr:o/r#7", Repo: "o/r", Number: 7, Title: "T",
				URL: "u", State: "open", Draft: true, Checks: "pending", Review: "none", UpdatedAt: "x"},
		},
	}
	if err := SaveGHPRs(in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadGHPRs()
	if err != nil {
		t.Fatal(err)
	}
	if out.FetchedAt != in.FetchedAt || len(out.PRs) != 1 || out.PRs["gh:pr:o/r#7"] != in.PRs["gh:pr:o/r#7"] {
		t.Fatalf("round trip = %+v", out)
	}
	// Atomic replace leaves no temp files behind.
	entries, _ := os.ReadDir(filepath.Join(dir, "state"))
	if len(entries) != 1 || entries[0].Name() != "gh-prs.json" {
		t.Fatalf("state dir = %v, want only gh-prs.json", entries)
	}
}

func TestLoadCorruptIsAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYLOG_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state", "gh-prs.json"), []byte("{torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGHPRs(); err == nil {
		t.Fatal("corrupt snapshot must surface as an error, not silent data")
	}
}
