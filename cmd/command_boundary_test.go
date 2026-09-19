package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAssistantCommandsUnavailable(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{
		{"athena"},
		{"athena", "memory", "list"},
		{"athena", "dreams", "list"},
		{"memory", "list"},
		{"dreams", "list"},
	} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			out, err := runCLI(t, home, "human:cli", args...)
			if err == nil || !strings.Contains(out, `unknown command "`+args[0]+`" for "daylog"`) {
				t.Fatalf("removed command was not rejected: %s (%v)", out, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, "data")); !os.IsNotExist(err) {
		t.Fatalf("rejected commands touched the journal store: %v", err)
	}
	for _, command := range rootCmd.Commands() {
		for _, name := range append([]string{command.Name()}, command.Aliases...) {
			if name == "athena" || name == "memory" || name == "dreams" {
				t.Fatalf("assistant command or alias still registered: %s", name)
			}
		}
	}
}

func TestJournalWorksWithoutAssistantCommands(t *testing.T) {
	home := t.TempDir()
	const note = "Human journal notes remain available"
	out, err := runCLI(t, home, "human:cli", "add", note)
	if err != nil || !strings.HasPrefix(out, "logged ") {
		t.Fatalf("human note failed: %s (%v)", out, err)
	}
	const report = "Agent report stays private pending curation"
	out, err = runCLI(t, home, "agent:pi", "add", report)
	if err != nil || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("agent capture failed: %s (%v)", out, err)
	}
	before, err := runCLI(t, home, "human:cli", "today", "--json")
	if err != nil || !strings.Contains(before, note) || strings.Contains(before, report) {
		t.Fatalf("journal/capture boundary changed: %s (%v)", before, err)
	}
	if out, err := runCLI(t, home, "human:cli", "athena", "memory", "list"); err == nil {
		t.Fatalf("assistant command unexpectedly succeeded: %s", out)
	}
	after, err := runCLI(t, home, "human:cli", "today", "--json")
	if err != nil {
		t.Fatalf("journal read failed: %s (%v)", after, err)
	}
	var beforeDay, afterDay map[string]any
	if err := json.Unmarshal([]byte(before), &beforeDay); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(after), &afterDay); err != nil {
		t.Fatal(err)
	}
	// Each read generates a fresh response timestamp, not a journal mutation.
	delete(beforeDay, "generated_at")
	delete(afterDay, "generated_at")
	if !reflect.DeepEqual(beforeDay, afterDay) {
		t.Fatalf("rejected assistant command changed journal: %s", after)
	}
	out, err = runCLI(t, home, "human:cli", "queue", "list", "--status", "pending")
	if err != nil || !strings.Contains(out, report) {
		t.Fatalf("queued report was not retained: %s (%v)", out, err)
	}
}
