package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJournalReadsNeverInitializeStores(t *testing.T) {
	for _, args := range [][]string{{"today", "--json"}, {"today"}, {"render"}, {"days", "--json"}} {
		t.Run(args[0]+"-"+args[len(args)-1], func(t *testing.T) {
			home := t.TempDir()
			if out, err := runCLI(t, home, "human:widget", args...); err == nil {
				t.Fatal("missing store read succeeded", out)
			}
			for _, path := range []string{"data", "data.init.lock"} {
				if _, err := os.Stat(filepath.Join(home, path)); !os.IsNotExist(err) {
					t.Fatal("read created", path, err)
				}
			}
			if out, err := runCLI(t, home, "human:cli", "init"); err != nil {
				t.Fatal(out, err)
			}
			if out, err := runCLI(t, home, "human:widget", args...); err != nil {
				t.Fatal("existing empty store must remain readable", out, err)
			}
		})
	}
}
