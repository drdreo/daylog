package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAllExistingDoesNotInitialize(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	t.Setenv("DAYLOG_DIR", root)
	if _, err := ReadAllExisting(); err == nil {
		t.Fatal("missing store must fail")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("read created store", err)
	}
}

func TestReadAllExistingValidatesAndRejectsCorruption(t *testing.T) {
	t.Setenv("DAYLOG_DIR", t.TempDir())
	e := sample()
	if err := Append(e); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadAllExisting()
	if err != nil || len(entries) != 1 || entries[0].ID != e.ID {
		t.Fatal(entries, err)
	}
	root, _ := DataDir()
	path := filepath.Join(root, "store.json")
	marker, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"protocol":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAllExisting(); err == nil {
		t.Fatal("unsupported marker must fail")
	}
	if err := os.WriteFile(path, marker, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "events", "1900-01-01.jsonl"), []byte("not-json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAllExisting(); err == nil {
		t.Fatal("corruption must not become missing calendar markers")
	}
}
