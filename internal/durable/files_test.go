package durable

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{"a":1} trailing`, `{"a":1} {"a":2}`} {
		var v map[string]any
		if Decode([]byte(raw), &v) == nil {
			t.Fatal(raw)
		}
	}
}
func TestAtomicFilesAndUnrelatedParentPermissions(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		os.Chmod(dir, 0755)
	}
	p := filepath.Join(dir, "private", "value.json")
	if err := JSON(p, map[string]int{"version": 2}); err != nil {
		t.Fatal(err)
	}
	var v map[string]int
	if err := Read(p, &v); err != nil || v["version"] != 2 {
		t.Fatal(v, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(dir)
		if info.Mode().Perm() != 0755 {
			t.Fatal("changed unrelated parent mode")
		}
		info, _ = os.Stat(p)
		if info.Mode().Perm() != 0600 {
			t.Fatal("non-private file")
		}
		info, _ = os.Stat(filepath.Dir(p))
		if info.Mode().Perm() != 0700 {
			t.Fatal("non-private directory")
		}
	}
}
