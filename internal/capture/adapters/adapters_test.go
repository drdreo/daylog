package adapters

import (
	"encoding/json"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sourceFixture(t *testing.T, h string) (*capture.Spool, config.Config, string, string) {
	t.Helper()
	t.Setenv("DAYLOG_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("DAYLOG_INTERNAL", "")
	q, e := capture.Open()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	project := t.TempDir()
	b, e := os.ReadFile("../../../testdata/gatekeeper/" + h + "-" + Versions[h] + ".jsonl")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "session.jsonl")
	os.WriteFile(path, []byte(strings.ReplaceAll(string(b), "/approved", filepath.ToSlash(project))), 0600)
	cfg := config.Defaults()
	cfg.CaptureScopes = []config.Scope{{Harness: h, Directory: dir, Projects: []string{project}}}
	return q, cfg, path, project
}
func TestNativeFixturesAndIncrementalCursor(t *testing.T) {
	for _, h := range []string{"pi", "claude", "codex"} {
		t.Run(h, func(t *testing.T) {
			q, cfg, path, _ := sourceFixture(t, h)
			n, e := ScanFile(q, cfg, h, path, time.Now())
			if e != nil || n == 0 {
				t.Fatal(n, e)
			}
			n, e = ScanFile(q, cfg, h, path, time.Now())
			if e != nil || n != 0 {
				t.Fatal("replay", n, e)
			}
			items, e := q.List("")
			if e != nil {
				t.Fatal(e)
			}
			for _, it := range items {
				if strings.Contains(it.Candidate.Text, "PRIVATE") || strings.Contains(it.Candidate.Text, "SECRET") {
					t.Fatal("hidden payload leaked")
				}
				if it.Candidate.Context.Cwd == "" {
					t.Fatal("missing original cwd")
				}
			}
		})
	}
}
func TestHookTranscriptOverlapAndRepeatedStops(t *testing.T) {
	for _, h := range []string{"pi", "claude", "codex"} {
		t.Run(h, func(t *testing.T) {
			q, cfg, path, project := sourceFixture(t, h)
			hook := Hook{AdapterVersion: 1, HarnessVersion: Versions[h], Event: "Stop", Session: h + "-session", Cwd: project, Text: "Fixed refresh locking; deployment not attempted.", Timestamp: "2026-09-06T10:00:03Z", Turn: "turn1", Prompt: "prompt1", Terminal: "completed"}
			if h == "pi" {
				hook.Event = "agent_settled"
				hook.NativeID = "final1"
				hook.Turn = "user1"
			}
			b, _ := json.Marshal(hook)
			a, e := CaptureHook(q, cfg, h, Versions[h], b, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			b2, e := CaptureHook(q, cfg, h, Versions[h], b, time.Now())
			if e != nil || a.ID != b2.ID {
				t.Fatal(e)
			}
			_, e = ScanFile(q, cfg, h, path, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			items, _ := q.List("")
			want := 1
			if h == "pi" {
				want = 2
			}
			if len(items) != want {
				t.Fatalf("overlap duplicated inputs %d want %d", len(items), want)
			}
		})
	}
}
func TestPiCopiedHistoryAndBranchTurns(t *testing.T) {
	q, cfg, path, _ := sourceFixture(t, "pi")
	ScanFile(q, cfg, "pi", path, time.Now())
	b, _ := os.ReadFile(path)
	copyPath := filepath.Join(filepath.Dir(path), "copy.jsonl")
	b = []byte(strings.Replace(string(b), `"id":"pi-session"`, `"id":"fork-session","parentSession":"original.jsonl"`, 1))
	os.WriteFile(copyPath, b, 0600)
	if _, e := ScanFile(q, cfg, "pi", copyPath, time.Now()); e != nil {
		t.Fatal(e)
	}
	items, _ := q.List("")
	if len(items) != 2 {
		t.Fatal("copied ancestry recaptured", len(items))
	}
	if items[0].Candidate.Episode == items[1].Candidate.Episode {
		t.Fatal("branched requests conflated")
	}
}
func TestPartialNativeTailAndDeletedWorktree(t *testing.T) {
	q, cfg, path, project := sourceFixture(t, "pi")
	os.Remove(project)
	b, _ := os.ReadFile(path)
	last := strings.LastIndex(string(b[:len(b)-1]), "\n") + 1
	os.WriteFile(path, b[:last+10], 0600)
	if _, e := ScanFile(q, cfg, "pi", path, time.Now()); e == nil {
		t.Fatal("partial tail not diagnosed")
	}
	os.WriteFile(path, b, 0600)
	if _, e := ScanFile(q, cfg, "pi", path, time.Now()); e != nil {
		t.Fatal(e)
	}
	items, _ := q.List("")
	if len(items) != 2 || items[0].Candidate.Context.Cwd != project {
		t.Fatal(items)
	}
}
func TestPrivacyUnsupportedAndUnapproved(t *testing.T) {
	q, cfg, path, project := sourceFixture(t, "codex")
	cfg.CaptureScopes[0].Projects = []string{t.TempDir()}
	if _, e := ScanFile(q, cfg, "codex", path, time.Now()); e == nil {
		t.Fatal("scope bypass")
	}
	h := Hook{Event: "Stop", Session: "s", Turn: "t", Cwd: project, Text: "outcome"}
	b, _ := json.Marshal(h)
	if _, e := CaptureHook(q, cfg, "codex", "old", b, time.Now()); e == nil {
		t.Fatal("old version")
	}
	if strings.Contains(Redact("safe\nAPI_KEY=abc\n.env values\npassword=secret"), "abc") {
		t.Fatal("redaction failed")
	}
}
