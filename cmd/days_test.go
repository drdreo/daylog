package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/view"
)

func TestDaysCLI(t *testing.T) {
	home := t.TempDir()
	// A read must not silently initialize a store.
	if _, err := runCLI(t, home, "human:cli", "days", "--json"); err == nil {
		t.Fatal("expected missing-store error")
	}
	if _, err := os.Stat(filepath.Join(home, "data", "store.json")); !os.IsNotExist(err) {
		t.Fatal("calendar read created a store", err)
	}
	if out, err := runCLI(t, home, "human:cli", "init"); err != nil {
		t.Fatal(out, err)
	}
	for _, args := range [][]string{
		{"add", "--type", "note", "Calendar note"},
		{"add", "--type", "todo", "Open task"},
	} {
		if out, err := runCLI(t, home, "human:cli", args...); err != nil {
			t.Fatal(out, err)
		}
	}
	out, err := runCLI(t, home, "human:cli", "today", "--json")
	if err != nil {
		t.Fatal(out, err)
	}
	var day view.Day
	if err := json.Unmarshal([]byte(out), &day); err != nil {
		t.Fatal(err)
	}
	check := func(count int) {
		t.Helper()
		out, err := runCLI(t, home, "human:cli", "days", day.Date[:7], "--json")
		var month view.Month
		if err != nil || json.Unmarshal([]byte(out), &month) != nil || month.Version != 2 || len(month.Days) != 1 || month.Days[0].Date != day.Date || month.Days[0].Count != count {
			t.Fatal(out, err)
		}
	}
	check(1)
	if out, err := runCLI(t, home, "human:cli", "done", day.OpenTodos[0].ID); err != nil {
		t.Fatal(out, err)
	}
	check(2)
	for _, value := range []string{"2026-13", "2026-2", "2026-09-06", "nonsense"} {
		if out, err := runCLI(t, home, "human:cli", "days", value, "--json"); err == nil || !strings.Contains(out, "expected YYYY-MM") {
			t.Fatal(value, out, err)
		}
	}
	out, err = runCLI(t, home, "human:cli", "days", "1900-01", "--json")
	if err != nil || !strings.Contains(out, `"days":[]`) {
		t.Fatal(out, err)
	}
}
